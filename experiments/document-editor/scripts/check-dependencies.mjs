import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const packageJSON = JSON.parse(
    readFileSync(new URL("../package.json", import.meta.url), "utf8"),
);
const lock = JSON.parse(
    readFileSync(new URL("../package-lock.json", import.meta.url), "utf8"),
);
const npmrc = readFileSync(new URL("../.npmrc", import.meta.url), "utf8");

const allowedLicenses = new Set([
    "Apache-2.0",
    "BSD-2-Clause",
    "BSD-3-Clause",
    "ISC",
    "MIT",
    "MPL-2.0",
]);
const reviewedLifecyclePackages = new Map([
    [
        "node_modules/fsevents",
        {
            version: "2.3.3",
            integrity:
                "sha512-5xoDfX+fL7faATnagmWPpbFtwh/R77WmMMqqHGS65C3vvB0YHrgF+B1YmZ3441tMj5n63k0212XNoJwzlhffQw==",
            optional: true,
        },
    ],
]);
const lifecycleNames = ["preinstall", "install", "postinstall"];
const requiredNpmrc = [
    "audit=true",
    "engine-strict=true",
    "fund=false",
    "ignore-scripts=true",
    "package-lock=true",
    "registry=https://registry.npmjs.org/",
];

assert.equal(
    lock.lockfileVersion,
    3,
    "package-lock.json must use lockfileVersion 3",
);
assert.deepEqual(
    lock.packages[""].dependencies,
    packageJSON.dependencies,
    "lockfile root dependencies must exactly match package.json",
);
assert.deepEqual(
    lock.packages[""].devDependencies,
    packageJSON.devDependencies,
    "lockfile root devDependencies must exactly match package.json",
);
for (const line of requiredNpmrc) {
    assert.ok(npmrc.split("\n").includes(line), `.npmrc must contain ${line}`);
}

const dependencies = Object.entries(lock.packages).filter(
    ([path]) => path !== "",
);
const licenses = new Set();
const seenReviewedLifecyclePackages = new Set();
for (const [path, metadata] of dependencies) {
    assert.match(
        metadata.resolved ?? "",
        /^https:\/\/registry\.npmjs\.org\//,
        `${path} must resolve from the official npm registry`,
    );
    assert.match(
        metadata.integrity ?? "",
        /^sha512-/,
        `${path} must use SHA-512 integrity`,
    );
    assert.ok(
        metadata.license,
        `${path} must declare a license in the lockfile`,
    );
    assert.ok(
        allowedLicenses.has(metadata.license),
        `${path} uses unreviewed license ${metadata.license}`,
    );
    if (metadata.hasInstallScript === true) {
        assert.deepEqual(
            {
                version: metadata.version,
                integrity: metadata.integrity,
                optional: metadata.optional,
            },
            reviewedLifecyclePackages.get(path),
            `${path} declares an unreviewed lifecycle install script`,
        );
        seenReviewedLifecyclePackages.add(path);
    }
    licenses.add(metadata.license);

    try {
        const installedPackage = JSON.parse(
            readFileSync(
                new URL(`../${path}/package.json`, import.meta.url),
                "utf8",
            ),
        );
        for (const lifecycleName of lifecycleNames) {
            if (reviewedLifecyclePackages.has(path)) continue;
            assert.equal(
                installedPackage.scripts?.[lifecycleName],
                undefined,
                `${path} declares ${lifecycleName}`,
            );
        }
    } catch (error) {
        if (error?.code !== "ENOENT") throw error;
    }
}

assert.deepEqual(
    [...seenReviewedLifecyclePackages].sort(),
    [...reviewedLifecyclePackages.keys()].sort(),
    "reviewed lifecycle package allowlist must exactly match the lockfile",
);

console.log(
    `dependency check passed: ${dependencies.length} lockfile packages; licenses=${[...licenses].sort().join(",")}; reviewed lifecycle manifests=${reviewedLifecyclePackages.size}; ignore-scripts=true`,
);
