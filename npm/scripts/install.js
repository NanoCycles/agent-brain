const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const https = require("node:https");
const { spawnSync } = require("node:child_process");

const version = process.env.npm_package_version;
const repo = process.env.AGENT_BRAIN_REPO || "NanoCycles/agent-brain";
const platformMap = { win32: "windows", darwin: "darwin", linux: "linux" };
const archMap = { x64: "amd64", arm64: "arm64" };
const goos = platformMap[process.platform];
const goarch = archMap[process.arch];

if (!goos || !goarch) {
  throw new Error(`Unsupported platform: ${process.platform}/${process.arch}`);
}

const ext = goos === "windows" ? "zip" : "tar.gz";
const asset = `agent-brain_${goos}_${goarch}.${ext}`;
const url = `https://github.com/${repo}/releases/download/v${version}/${asset}`;
const root = path.join(__dirname, "..");
const vendor = path.join(root, "vendor");
const tmp = path.join(os.tmpdir(), `agent-brain-npm-${Date.now()}-${asset}`);

fs.mkdirSync(vendor, { recursive: true });

download(url, tmp).then(() => {
  if (goos === "windows") {
    run("powershell", ["-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", `Expand-Archive -Path '${tmp}' -DestinationPath '${vendor}' -Force`]);
  } else {
    run("tar", ["-xzf", tmp, "-C", vendor]);
    fs.chmodSync(path.join(vendor, "agent-brain"), 0o755);
  }
  fs.rmSync(tmp, { force: true });
  console.log(`agent-brain v${version} installed for ${goos}/${goarch}`);
}).catch((err) => {
  console.error(err.message);
  process.exit(1);
});

function download(url, dest) {
  return new Promise((resolve, reject) => {
    https.get(url, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return download(res.headers.location, dest).then(resolve, reject);
      }
      if (res.statusCode !== 200) {
        reject(new Error(`Download failed ${res.statusCode}: ${url}`));
        return;
      }
      const file = fs.createWriteStream(dest);
      res.pipe(file);
      file.on("finish", () => file.close(resolve));
      file.on("error", reject);
    }).on("error", reject);
  });
}

function run(command, args) {
  const result = spawnSync(command, args, { stdio: "inherit" });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${command} failed with exit code ${result.status}`);
}
