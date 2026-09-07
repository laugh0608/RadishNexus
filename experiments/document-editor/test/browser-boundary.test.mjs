import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const browserSources = [
    new URL("../index.html", import.meta.url),
    new URL("../browser/main.mjs", import.meta.url),
    new URL("../browser/styles.css", import.meta.url),
].map((url) => readFileSync(url, "utf8"));

test("browser harness does not use storage or realtime transport", () => {
    const forbiddenAPIs = [
        "localStorage",
        "sessionStorage",
        "indexedDB",
        "caches.open",
        "EventSource",
        "WebSocket",
    ];
    for (const forbiddenAPI of forbiddenAPIs) {
        assert.equal(
            browserSources.some((source) => source.includes(forbiddenAPI)),
            false,
            `browser harness must not reference ${forbiddenAPI}`,
        );
    }
});
