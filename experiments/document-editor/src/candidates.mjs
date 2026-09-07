import { createHash } from "node:crypto";

import { CodeNode } from "@lexical/code";
import { createHeadlessEditor } from "@lexical/headless";
import { LinkNode } from "@lexical/link";
import { ListItemNode, ListNode } from "@lexical/list";
import {
    $convertFromMarkdownString,
    $convertToMarkdownString,
    TRANSFORMERS,
} from "@lexical/markdown";
import { HeadingNode, QuoteNode } from "@lexical/rich-text";
import { MarkdownManager } from "@tiptap/markdown";
import StarterKit from "@tiptap/starter-kit";

const tiptapManager = new MarkdownManager({ extensions: [StarterKit] });
const lexicalNodes = [
    HeadingNode,
    QuoteNode,
    ListItemNode,
    ListNode,
    LinkNode,
    CodeNode,
];
const tiptapNodeTypes = new Set([
    "doc",
    "text",
    "paragraph",
    "heading",
    "blockquote",
    "bulletList",
    "orderedList",
    "listItem",
    "codeBlock",
    "hardBreak",
    "horizontalRule",
]);
const tiptapMarkTypes = new Set([
    "bold",
    "code",
    "italic",
    "link",
    "strike",
    "underline",
]);

const tiptapRoles = new Map([
    ["paragraph", "paragraph"],
    ["heading", "heading"],
    ["blockquote", "blockquote"],
    ["bulletList", "unordered-list"],
    ["orderedList", "ordered-list"],
    ["listItem", "list-item"],
    ["codeBlock", "code-block"],
    ["hardBreak", "hard-break"],
]);

function sha256(value) {
    return createHash("sha256").update(value).digest("hex");
}

function summarizeTiptap(value) {
    const roles = new Set();
    let text = "";

    function visit(node) {
        if (!node || typeof node !== "object") return;
        if (typeof node.text === "string") text += node.text;
        const role = tiptapRoles.get(node.type);
        if (role) roles.add(role);
        for (const mark of node.marks ?? []) {
            if (mark.type === "link") roles.add("link");
        }
        for (const child of node.content ?? []) visit(child);
    }

    visit(value);
    return { roles: [...roles].sort(), text };
}

function summarizeLexical(value) {
    const roles = new Set();
    let text = "";

    function visit(node) {
        if (!node || typeof node !== "object") return;
        if (typeof node.text === "string") text += node.text;
        if (node.type === "paragraph") roles.add("paragraph");
        if (node.type === "heading") roles.add("heading");
        if (node.type === "quote") roles.add("blockquote");
        if (node.type === "list") {
            roles.add(
                node.listType === "number" ? "ordered-list" : "unordered-list",
            );
        }
        if (node.type === "listitem") roles.add("list-item");
        if (node.type === "code") roles.add("code-block");
        if (node.type === "linebreak" && node.$?.mdHardLineBreak)
            roles.add("hard-break");
        if (node.type === "link") roles.add("link");
        for (const child of node.children ?? []) visit(child);
    }

    visit(value.root);
    return { roles: [...roles].sort(), text };
}

function parseLexical(markdown) {
    const errors = [];
    const editor = createHeadlessEditor({
        namespace: "radishnexus-document-editor-experiment",
        nodes: lexicalNodes,
        onError(error) {
            errors.push(error);
        },
    });
    editor.update(() => $convertFromMarkdownString(markdown, TRANSFORMERS), {
        discrete: true,
    });
    if (errors.length > 0) throw errors[0];
    return editor.getEditorState().toJSON();
}

function assertTiptapSchema(node, path = "$") {
    if (!node || typeof node !== "object" || !tiptapNodeTypes.has(node.type)) {
        throw new Error(
            `unsupported Tiptap node at ${path}: ${node?.type ?? typeof node}`,
        );
    }
    for (const [index, mark] of (node.marks ?? []).entries()) {
        if (
            !mark ||
            typeof mark !== "object" ||
            !tiptapMarkTypes.has(mark.type)
        ) {
            throw new Error(
                `unsupported Tiptap mark at ${path}.marks[${index}]: ${mark?.type}`,
            );
        }
    }
    for (const [index, child] of (node.content ?? []).entries()) {
        assertTiptapSchema(child, `${path}.content[${index}]`);
    }
}

function serializeTiptap(json) {
    assertTiptapSchema(json);
    return tiptapManager.serialize(json);
}

function serializeLexical(json) {
    const errors = [];
    const editor = createHeadlessEditor({
        namespace: "radishnexus-document-editor-experiment",
        nodes: lexicalNodes,
        onError(error) {
            errors.push(error);
        },
    });
    editor.setEditorState(editor.parseEditorState(json));
    let markdown = "";
    editor.getEditorState().read(() => {
        markdown = $convertToMarkdownString(TRANSFORMERS);
    });
    if (errors.length > 0) throw errors[0];
    return markdown;
}

const candidates = {
    tiptap: {
        parse: (markdown) => tiptapManager.parse(markdown),
        serialize: serializeTiptap,
        summarize: summarizeTiptap,
    },
    lexical: {
        parse: parseLexical,
        serialize: serializeLexical,
        summarize: summarizeLexical,
    },
};

function buildDiagnostics(testCase, result) {
    const diagnostics = [];
    if (result.normalized !== testCase.markdown)
        diagnostics.push("normalized-input");
    if (!result.stable) diagnostics.push("unstable-second-round-trip");
    if (!result.parseDeterministic) diagnostics.push("non-deterministic-parse");

    for (const fragment of testCase.expectedText) {
        if (!result.structure.text.includes(fragment))
            diagnostics.push(`missing-text:${fragment}`);
    }
    for (const role of testCase.expectedRoles) {
        if (!result.structure.roles.includes(role))
            diagnostics.push(`missing-role:${role}`);
    }

    if (testCase.id === "raw-html") {
        if (result.normalized.includes("<custom-widget"))
            diagnostics.push("raw-html-preserved");
        else if (result.normalized.includes("&lt;custom-widget"))
            diagnostics.push("raw-html-escaped");
        else diagnostics.push("raw-html-lost");
    }
    if (testCase.id === "dangerous-link") {
        const serializedInternal = JSON.stringify(result.internal);
        if (
            serializedInternal.includes("javascript:") ||
            result.normalized.includes("javascript:")
        ) {
            diagnostics.push("dangerous-protocol-preserved-requires-sanitizer");
        } else {
            diagnostics.push("dangerous-protocol-removed");
        }
    }
    if (testCase.support === "unsupported")
        diagnostics.push("unsupported-input-explicitly-classified");
    return diagnostics;
}

export const candidateNames = Object.keys(candidates);

export function evaluateCandidate(candidateName, testCase) {
    const candidate = candidates[candidateName];
    if (!candidate) throw new Error(`unknown candidate: ${candidateName}`);

    const startedAt = performance.now();
    const internal = candidate.parse(testCase.markdown);
    const repeatedInternal = candidate.parse(testCase.markdown);
    const normalized = candidate.serialize(internal);
    const secondInternal = candidate.parse(normalized);
    const secondRoundTrip = candidate.serialize(secondInternal);
    const structure = candidate.summarize(internal);
    const internalJSON = JSON.stringify(internal);
    const result = {
        candidate: candidateName,
        case: testCase.id,
        support: testCase.support,
        input: testCase.markdown,
        internal,
        internalSHA256: sha256(internalJSON),
        normalized,
        secondRoundTrip,
        stable: normalized === secondRoundTrip,
        parseDeterministic: internalJSON === JSON.stringify(repeatedInternal),
        structure,
        elapsedMilliseconds: Number((performance.now() - startedAt).toFixed(3)),
    };
    return { ...result, diagnostics: buildDiagnostics(testCase, result) };
}

export function probeUnknownNode(candidateName) {
    if (candidateName === "tiptap") {
        const unknown = {
            type: "doc",
            content: [
                {
                    type: "futureCallout",
                    attrs: { kind: "note" },
                    content: [{ type: "text", text: "future payload" }],
                },
            ],
        };
        const nativeOutput = tiptapManager.serialize(unknown);
        let guardedOutcome = "accepted";
        let guardError;
        try {
            serializeTiptap(unknown);
        } catch (error) {
            guardedOutcome = "rejected";
            guardError = error instanceof Error ? error.message : String(error);
        }
        return {
            candidate: candidateName,
            nativeOutcome:
                nativeOutput === "" ? "silently-dropped" : "preserved",
            nativeOutput,
            guardedOutcome,
            guardError,
        };
    }

    const unknown = {
        root: {
            children: [{ type: "futureCallout", version: 1, children: [] }],
            direction: null,
            format: "",
            indent: 0,
            type: "root",
            version: 1,
        },
    };
    try {
        serializeLexical(unknown);
        return {
            candidate: candidateName,
            nativeOutcome: "accepted",
            guardedOutcome: "accepted",
        };
    } catch (error) {
        return {
            candidate: candidateName,
            nativeOutcome: "rejected",
            error: error instanceof Error ? error.message : String(error),
            guardedOutcome: "rejected",
        };
    }
}
