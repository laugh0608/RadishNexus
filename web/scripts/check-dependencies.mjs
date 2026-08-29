import { readFile } from "node:fs/promises";

const reviewedLicenses = new Set([
    "Apache-2.0",
    "BSD-2-Clause",
    "BSD-3-Clause",
    "BlueOak-1.0.0",
    "CC0-1.0",
    "ISC",
    "MIT",
    "MIT-0",
    "MPL-2.0",
]);

const reviewedInstallScripts = new Set(["node_modules/fsevents"]);
const registryPrefix = "https://registry.npmjs.org/";

const [manifestSource, lockSource, npmrcSource] = await Promise.all([
    readFile(new URL("../package.json", import.meta.url), "utf8"),
    readFile(new URL("../package-lock.json", import.meta.url), "utf8"),
    readFile(new URL("../.npmrc", import.meta.url), "utf8"),
]);

const manifest = JSON.parse(manifestSource);
const lock = JSON.parse(lockSource);
const errors = [];

if (lock.lockfileVersion !== 3) {
    errors.push(
        `package-lock.json 必须使用 lockfileVersion 3，实际为 ${lock.lockfileVersion}`,
    );
}

const root = lock.packages?.[""];
if (!root) {
    errors.push("package-lock.json 缺少根 package 条目");
} else {
    compareDependencyMap(
        "dependencies",
        manifest.dependencies,
        root.dependencies,
        errors,
    );
    compareDependencyMap(
        "devDependencies",
        manifest.devDependencies,
        root.devDependencies,
        errors,
    );
}

for (const [packagePath, metadata] of Object.entries(lock.packages ?? {})) {
    if (packagePath === "") {
        continue;
    }

    if (metadata.link) {
        errors.push(`${packagePath} 不应通过本地 link 进入 Web 依赖图`);
        continue;
    }

    if (!metadata.resolved?.startsWith(registryPrefix)) {
        errors.push(
            `${packagePath} 未从官方 npm registry 解析: ${metadata.resolved ?? "<missing>"}`,
        );
    }

    if (
        typeof metadata.integrity !== "string" ||
        !metadata.integrity.startsWith("sha512-")
    ) {
        errors.push(`${packagePath} 缺少 sha512 integrity`);
    }

    if (!reviewedLicenses.has(metadata.license)) {
        errors.push(
            `${packagePath} 使用了未审阅许可证: ${metadata.license ?? "<missing>"}`,
        );
    }

    if (metadata.hasInstallScript && !reviewedInstallScripts.has(packagePath)) {
        errors.push(`${packagePath} 声明了未审阅 lifecycle script`);
    }
}

const requiredNpmSettings = [
    "audit=true",
    "engine-strict=true",
    "fund=false",
    "ignore-scripts=true",
    "package-lock=true",
];
for (const setting of requiredNpmSettings) {
    if (!npmrcSource.split(/\r?\n/u).includes(setting)) {
        errors.push(`.npmrc 缺少安全基线: ${setting}`);
    }
}

if (errors.length > 0) {
    console.error("Web 依赖检查失败：");
    for (const error of errors) {
        console.error(`- ${error}`);
    }
    process.exit(1);
}

const packageCount = Math.max(Object.keys(lock.packages ?? {}).length - 1, 0);
console.log(
    `Web 依赖检查通过：${packageCount} 个锁定 package，来源、integrity、许可证与 lifecycle script 均符合基线。`,
);

function compareDependencyMap(label, manifestMap, lockMap, comparisonErrors) {
    const manifestEntries = Object.entries(manifestMap ?? {}).sort(
        ([left], [right]) => left.localeCompare(right),
    );
    const lockEntries = Object.entries(lockMap ?? {}).sort(([left], [right]) =>
        left.localeCompare(right),
    );

    if (JSON.stringify(manifestEntries) !== JSON.stringify(lockEntries)) {
        comparisonErrors.push(
            `package.json 与 package-lock.json 的 ${label} 不一致`,
        );
    }
}
