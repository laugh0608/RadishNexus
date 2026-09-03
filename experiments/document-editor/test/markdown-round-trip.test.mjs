import assert from "node:assert/strict";
import { describe, test } from "node:test";

import {
    browserOnlyCases,
    corpusVersion,
    markdownCases,
} from "../corpus/markdown-corpus.mjs";
import {
    candidateNames,
    evaluateCandidate,
    probeUnknownNode,
} from "../src/candidates.mjs";

describe(`shared Markdown corpus ${corpusVersion}`, () => {
    for (const candidate of candidateNames) {
        describe(candidate, () => {
            for (const testCase of markdownCases) {
                test(testCase.id, () => {
                    const result = evaluateCandidate(candidate, testCase);
                    assert.equal(
                        result.stable,
                        true,
                        result.diagnostics.join(", "),
                    );
                    assert.equal(
                        result.parseDeterministic,
                        true,
                        result.diagnostics.join(", "),
                    );
                    for (const fragment of testCase.expectedText) {
                        assert.ok(
                            result.structure.text.includes(fragment),
                            `${candidate}/${testCase.id} lost text ${JSON.stringify(fragment)}`,
                        );
                    }
                    for (const role of testCase.expectedRoles) {
                        assert.ok(
                            result.structure.roles.includes(role),
                            `${candidate}/${testCase.id} lost role ${role}`,
                        );
                    }
                    if (testCase.support === "unsupported") {
                        assert.ok(
                            result.diagnostics.includes(
                                "unsupported-input-explicitly-classified",
                            ),
                        );
                    }
                    if (testCase.id === "raw-html") {
                        assert.ok(
                            result.diagnostics.some((value) =>
                                value.startsWith("raw-html-"),
                            ),
                        );
                    }
                    if (testCase.id === "dangerous-link") {
                        assert.ok(
                            result.diagnostics.some(
                                (value) =>
                                    value === "dangerous-protocol-removed" ||
                                    value ===
                                        "dangerous-protocol-preserved-requires-sanitizer",
                            ),
                        );
                    }
                });
            }

            test("unknown versioned node is not accepted by the guarded contract", () => {
                const result = probeUnknownNode(candidate);
                assert.equal(result.guardedOutcome, "rejected");
            });
        });
    }

    test("browser-only acceptance surface remains explicit", () => {
        assert.deepEqual(browserOnlyCases, [
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
        ]);
    });
});
