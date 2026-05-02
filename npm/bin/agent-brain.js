#!/usr/bin/env node
const { spawnSync } = require("node:child_process");
const { existsSync } = require("node:fs");
const path = require("node:path");

const exe = process.platform === "win32" ? "agent-brain.exe" : "agent-brain";
const bin = path.join(__dirname, "..", "vendor", exe);

if (!existsSync(bin)) {
  console.error("agent-brain binary is missing. Reinstall @nanocycles/agent-brain.");
  process.exit(1);
}

const result = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}
process.exit(result.status ?? 0);
