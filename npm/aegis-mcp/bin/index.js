#!/usr/bin/env node
const { execFileSync } = require("child_process");
const os = require("os");
const path = require("path");

const PLATFORMS = {
  "linux-x64": "aegis-mcp-linux-x64",
  "linux-arm64": "aegis-mcp-linux-arm64",
  "darwin-x64": "aegis-mcp-darwin-x64",
  "darwin-arm64": "aegis-mcp-darwin-arm64",
};

const key = `${os.platform()}-${os.arch()}`;
const pkg = PLATFORMS[key];
if (!pkg) {
  console.error(`Unsupported platform: ${key}`);
  process.exit(1);
}

const bin = path.join(require.resolve(`${pkg}/package.json`), "..", "bin", "aegis");
try {
  execFileSync(bin, process.argv.slice(2), { stdio: "inherit" });
} catch (e) {
  if (e.code === "ENOENT") {
    console.error(`Binary not found: ${bin}`);
    console.error(`The platform package ${pkg} may not be installed correctly.`);
  }
  process.exit(e.status ?? 1);
}
