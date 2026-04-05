const fs = require("fs");
const path = require("path");

const out = path.join(__dirname, "..", "out");
const dest = path.join(__dirname, "..", "..", "main", "web");

if (!fs.existsSync(out)) {
  console.error("missing out/ — run next build first");
  process.exit(1);
}

fs.rmSync(dest, { recursive: true, force: true });
fs.cpSync(out, dest, { recursive: true });
