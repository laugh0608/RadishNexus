import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const packageJSON = JSON.parse(
    readFileSync(new URL("../package.json", import.meta.url), "utf8"),
);
const lock = JSON.parse(
    readFileSync(new URL("../package-lock.json", import.meta.url), "utf8"),
);
const npmrc = readFileSync(new URL("../.npmrc", import.meta.url), "utf8");

const allowedLicenses = new Set(["MIT", "BSD-2-Clause"]);
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
for (const line of requiredNpmrc) {
    assert.ok(npmrc.split("\n").includes(line), `.npmrc must contain ${line}`);
}

const dependencies = Object.entries(lock.packages).filter(
    ([path]) => path !== "",
);
const licenses = new Set();
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
    assert.notEqual(
        metadata.hasInstallScript,
        true,
        `${path} declares a lifecycle install script`,
    );
    licenses.add(metadata.license);

    try {
        const installedPackage = JSON.parse(
            readFileSync(
                new URL(`../${path}/package.json`, import.meta.url),
                "utf8",
            ),
        );
        for (const lifecycleName of lifecycleNames) {
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

console.log(
    `dependency check passed: ${dependencies.length} packages; licenses=${[...licenses].sort().join(",")}; lifecycle scripts=0`,
);
