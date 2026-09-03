export const corpusVersion = "radishnexus-markdown-v1";

const longURL = `https://example.com/${"segment-".repeat(48)}end?query=${"value".repeat(32)}`;
const largeSections = Array.from(
    { length: 400 },
    (_, index) =>
        `## Section ${index + 1}\n\nParagraph ${index + 1} 包含 emoji 🥕 and **bold text**.\n\n- item ${index + 1}.1\n- item ${index + 1}.2`,
).join("\n\n");

export const markdownCases = [
    {
        id: "empty",
        support: "supported",
        markdown: "",
        expectedText: [],
        expectedRoles: [],
    },
    {
        id: "paragraphs",
        support: "supported",
        markdown: "First paragraph.\n\nSecond paragraph.",
        expectedText: ["First paragraph.", "Second paragraph."],
        expectedRoles: ["paragraph"],
    },
    {
        id: "heading-emphasis-link-quote",
        support: "supported",
        markdown:
            '# Heading\n\nA **bold** and *italic* [safe link](https://example.com "Example").\n\n> Quoted text.',
        expectedText: [
            "Heading",
            "bold",
            "italic",
            "safe link",
            "Quoted text.",
        ],
        expectedRoles: ["heading", "link", "blockquote"],
    },
    {
        id: "ordered-unordered-nested-lists",
        support: "supported",
        markdown:
            "- first\n  - nested\n- second\n\n3. third\n4. fourth\n   1. nested ordered",
        expectedText: [
            "first",
            "nested",
            "second",
            "third",
            "fourth",
            "nested ordered",
        ],
        expectedRoles: ["unordered-list", "ordered-list", "list-item"],
    },
    {
        id: "fenced-and-inline-code",
        support: "supported",
        markdown:
            "Use `const value = 1` inline.\n\n```ts\nconst answer = 42\n```",
        expectedText: ["const value = 1", "const answer = 42"],
        expectedRoles: ["paragraph", "code-block"],
    },
    {
        id: "soft-and-hard-breaks",
        support: "supported",
        markdown: "soft one\nsoft two\n\nhard spaces  \nnext\\\nlast",
        expectedText: ["soft one", "soft two", "hard spaces", "next", "last"],
        expectedRoles: ["paragraph", "hard-break"],
    },
    {
        id: "unicode-mixed-direction",
        support: "supported",
        markdown: "中文、emoji 🥕、组合字符 e\u0301，RTL العربية עברית。",
        expectedText: ["中文", "🥕", "e\u0301", "العربية", "עברית"],
        expectedRoles: ["paragraph"],
    },
    {
        id: "long-word-and-url",
        support: "supported",
        markdown: `unbroken-${"x".repeat(512)}\n\n[long URL](${longURL})`,
        expectedText: ["unbroken-", "long URL"],
        expectedRoles: ["paragraph", "link"],
    },
    {
        id: "raw-html",
        support: "unsupported",
        markdown: '<custom-widget data-x="1">payload</custom-widget>',
        expectedText: ["payload"],
        expectedRoles: ["paragraph"],
    },
    {
        id: "dangerous-link",
        support: "unsupported",
        markdown: "[unsafe](javascript:alert(1))",
        expectedText: ["unsafe"],
        expectedRoles: ["paragraph"],
    },
    {
        id: "malformed-input",
        support: "unsupported",
        markdown:
            "Unclosed **bold and [link](https://example.com\n\n```ts\nunterminated",
        expectedText: ["Unclosed", "unterminated"],
        expectedRoles: ["paragraph", "code-block"],
    },
    {
        id: "large-document",
        support: "supported",
        markdown: `# Large document\n\n${largeSections}`,
        expectedText: [
            "Large document",
            "Section 1",
            "Section 400",
            "item 400.2",
        ],
        expectedRoles: ["heading", "paragraph", "unordered-list", "list-item"],
    },
];

export const browserOnlyCases = [
    "plain-text-paste",
    "markdown-paste",
    "keyboard-only",
    "composition-ime",
    "undo-redo",
    "focus-restore",
    "copy-paste",
    "screen-reader-semantics",
    "390px-layout",
    "long-document-interaction",
];
