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

const results = candidateNames.flatMap((candidate) =>
    markdownCases.map((testCase) => evaluateCandidate(candidate, testCase)),
);

console.log(
    JSON.stringify(
        {
            schema: "radishnexus-document-editor-headless-report/v1",
            corpusVersion,
            candidates: candidateNames,
            results,
            unknownNode: candidateNames.map(probeUnknownNode),
            browserOnlyCases,
        },
        null,
        2,
    ),
);
