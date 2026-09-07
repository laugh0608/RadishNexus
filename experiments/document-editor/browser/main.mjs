import { createEmptyHistoryState, registerHistory } from "@lexical/history";
import {
    INSERT_UNORDERED_LIST_COMMAND,
    ListItemNode,
    ListNode,
    registerList,
} from "@lexical/list";
import {
    $convertFromMarkdownString,
    $convertToMarkdownString,
    $generateNodesFromMarkdownString,
    TRANSFORMERS,
} from "@lexical/markdown";
import { CodeNode } from "@lexical/code";
import { LinkNode } from "@lexical/link";
import { HeadingNode, QuoteNode, registerRichText } from "@lexical/rich-text";
import {
    $getRoot,
    $getSelection,
    $isRangeSelection,
    CLEAR_HISTORY_COMMAND,
    createEditor,
    FORMAT_TEXT_COMMAND,
    REDO_COMMAND,
    UNDO_COMMAND,
} from "lexical";
import { Editor } from "@tiptap/core";
import { Markdown } from "@tiptap/markdown";
import StarterKit from "@tiptap/starter-kit";

import { corpusVersion, markdownCases } from "../corpus/markdown-corpus.mjs";

const sampleMarkdown = `# Browser acceptance

Paragraph with **bold**, 中文输入 and emoji 🥕.

- first item
- second item`;
const longMarkdown = markdownCases.find(
    ({ id }) => id === "large-document",
).markdown;

function requiredElement(selector) {
    const element = document.querySelector(selector);
    if (!element)
        throw new Error(`missing browser fixture element: ${selector}`);
    return element;
}

function createInstrumentation(candidate, root, status, renderMarkdown) {
    const state = {
        updates: 0,
        compositionStarts: 0,
        compositionUpdates: 0,
        compositionEnds: 0,
        lastCompositionData: "",
    };

    const render = () => {
        root.dataset.updateCount = String(state.updates);
        root.dataset.compositionStarts = String(state.compositionStarts);
        root.dataset.compositionUpdates = String(state.compositionUpdates);
        root.dataset.compositionEnds = String(state.compositionEnds);
        root.dataset.lastCompositionData = state.lastCompositionData;
        status.value = `${candidate}: updates ${state.updates}; composition ${state.compositionStarts}/${state.compositionUpdates}/${state.compositionEnds}`;
        renderMarkdown();
    };

    root.addEventListener("compositionstart", (event) => {
        state.compositionStarts += 1;
        state.lastCompositionData = event.data ?? "";
        render();
    });
    root.addEventListener("compositionupdate", (event) => {
        state.compositionUpdates += 1;
        state.lastCompositionData = event.data ?? "";
        render();
    });
    root.addEventListener("compositionend", (event) => {
        state.compositionEnds += 1;
        state.lastCompositionData = event.data ?? "";
        render();
    });

    return {
        state,
        updated() {
            state.updates += 1;
            render();
        },
        render,
    };
}

function bindToolbar(candidate, actions) {
    const toolbar = requiredElement(`[data-candidate="${candidate}"] .toolbar`);
    for (const button of toolbar.querySelectorAll("button[data-action]")) {
        button.addEventListener("click", () => {
            const action = actions[button.dataset.action];
            if (!action)
                throw new Error(
                    `unknown ${candidate} action: ${button.dataset.action}`,
                );
            action();
        });
    }
}

const tiptapMount = requiredElement('[data-editor="tiptap"]');
const tiptapMarkdownOutput = requiredElement('[data-markdown="tiptap"]');
const tiptapStatus = requiredElement('[data-status="tiptap"]');
let tiptapInstrumentation;
const tiptap = new Editor({
    element: tiptapMount,
    extensions: [StarterKit, Markdown],
    content: sampleMarkdown,
    contentType: "markdown",
    editorProps: {
        attributes: {
            "aria-label": "Tiptap document editor",
            "aria-multiline": "true",
            "data-browser-editor": "tiptap",
            role: "textbox",
            spellcheck: "false",
        },
    },
    onUpdate() {
        tiptapInstrumentation?.updated();
    },
});
const tiptapRoot = tiptap.view.dom;
tiptapInstrumentation = createInstrumentation(
    "tiptap",
    tiptapRoot,
    tiptapStatus,
    () => {
        tiptapMarkdownOutput.textContent = tiptap.getMarkdown();
    },
);
tiptapInstrumentation.render();

tiptapRoot.addEventListener(
    "paste",
    (event) => {
        const markdown = event.clipboardData?.getData("text/markdown");
        if (!markdown) return;
        event.preventDefault();
        event.stopImmediatePropagation();
        tiptap.commands.insertContent(markdown, { contentType: "markdown" });
    },
    true,
);

bindToolbar("tiptap", {
    bold: () => tiptap.chain().focus().toggleBold().run(),
    bullet: () => tiptap.chain().focus().toggleBulletList().run(),
    undo: () => tiptap.chain().focus().undo().run(),
    redo: () => tiptap.chain().focus().redo().run(),
    "load-sample": () => {
        tiptap.commands.setContent(sampleMarkdown, { contentType: "markdown" });
        tiptap.commands.focus("end");
    },
    "load-large": () => {
        tiptap.commands.setContent(longMarkdown, { contentType: "markdown" });
        tiptap.commands.focus("start");
    },
    clear: () => {
        tiptap.commands.clearContent();
        tiptap.commands.focus();
    },
});

const lexicalRoot = requiredElement('[data-editor="lexical"]');
const lexicalMarkdownOutput = requiredElement('[data-markdown="lexical"]');
const lexicalStatus = requiredElement('[data-status="lexical"]');
lexicalRoot.contentEditable = "true";
lexicalRoot.setAttribute("role", "textbox");
lexicalRoot.setAttribute("aria-label", "Lexical document editor");
lexicalRoot.setAttribute("aria-multiline", "true");
lexicalRoot.setAttribute("data-browser-editor", "lexical");
lexicalRoot.setAttribute("spellcheck", "false");

const lexical = createEditor({
    namespace: "radishnexus-document-editor-browser-experiment",
    nodes: [HeadingNode, QuoteNode, ListItemNode, ListNode, LinkNode, CodeNode],
    onError(error) {
        throw error;
    },
});
lexical.setRootElement(lexicalRoot);
registerRichText(lexical);
registerList(lexical);
registerHistory(lexical, createEmptyHistoryState(), 250);

const lexicalInstrumentation = createInstrumentation(
    "lexical",
    lexicalRoot,
    lexicalStatus,
    () => {
        lexical.getEditorState().read(() => {
            lexicalMarkdownOutput.textContent =
                $convertToMarkdownString(TRANSFORMERS);
        });
    },
);
lexical.registerUpdateListener(() => lexicalInstrumentation.updated());

function setLexicalMarkdown(
    markdown,
    clearHistory = false,
    resetSelectionFormat = false,
) {
    lexical.update(
        () => {
            $convertFromMarkdownString(markdown, TRANSFORMERS);
            const selection = $getSelection();
            if (resetSelectionFormat && $isRangeSelection(selection)) {
                selection.setFormat(0);
                selection.setStyle("");
            }
        },
        { discrete: true },
    );
    if (clearHistory) lexical.dispatchCommand(CLEAR_HISTORY_COMMAND, undefined);
}

setLexicalMarkdown(sampleMarkdown, true, true);
lexicalInstrumentation.render();

lexicalRoot.addEventListener(
    "paste",
    (event) => {
        const markdown = event.clipboardData?.getData("text/markdown");
        if (!markdown) return;
        event.preventDefault();
        event.stopImmediatePropagation();
        lexical.update(
            () => {
                const nodes = $generateNodesFromMarkdownString(
                    markdown,
                    TRANSFORMERS,
                );
                const selection = $getSelection();
                if ($isRangeSelection(selection)) selection.insertNodes(nodes);
                else $getRoot().append(...nodes);
            },
            { discrete: true },
        );
    },
    true,
);

bindToolbar("lexical", {
    bold: () => {
        lexical.focus(() =>
            lexical.dispatchCommand(FORMAT_TEXT_COMMAND, "bold"),
        );
    },
    bullet: () => {
        lexical.focus(() =>
            lexical.dispatchCommand(INSERT_UNORDERED_LIST_COMMAND, undefined),
        );
    },
    undo: () => {
        lexical.dispatchCommand(UNDO_COMMAND, undefined);
        lexical.focus();
    },
    redo: () => {
        lexical.dispatchCommand(REDO_COMMAND, undefined);
        lexical.focus();
    },
    "load-sample": () => {
        setLexicalMarkdown(sampleMarkdown);
        lexical.focus();
    },
    "load-large": () => {
        setLexicalMarkdown(longMarkdown);
        lexical.focus();
    },
    clear: () => {
        setLexicalMarkdown("", false, true);
        lexical.focus();
    },
});

function dispatchMarkdownPaste(candidate, markdown) {
    const root = candidate === "tiptap" ? tiptapRoot : lexicalRoot;
    root.focus();
    const clipboardData = new DataTransfer();
    clipboardData.setData("text/markdown", markdown);
    clipboardData.setData("text/plain", markdown);
    const event = new ClipboardEvent("paste", {
        bubbles: true,
        cancelable: true,
        clipboardData,
    });
    root.dispatchEvent(event);
    return event.defaultPrevented;
}

window.__radishEditorExperiment = Object.freeze({
    schema: "radishnexus-document-editor-browser/v1",
    corpusVersion,
    sampleMarkdown,
    longMarkdownLength: longMarkdown.length,
    candidates: {
        tiptap: {
            root: tiptapRoot,
            getMarkdown: () => tiptap.getMarkdown(),
            getJSON: () => tiptap.getJSON(),
            focus: () => tiptap.commands.focus(),
        },
        lexical: {
            root: lexicalRoot,
            getMarkdown: () => lexicalMarkdownOutput.textContent,
            getJSON: () => lexical.getEditorState().toJSON(),
            focus: () => lexical.focus(),
        },
    },
    dispatchMarkdownPaste,
});

document.documentElement.dataset.browserHarness = "ready";
