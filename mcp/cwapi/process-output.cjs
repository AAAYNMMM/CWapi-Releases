"use strict";

const fs = require("node:fs");

const secretPatterns = [
  /\b(?:xapp|xox[baprs])-[A-Za-z0-9-]+\b/gi,
  /\bgh[pousr]_[A-Za-z0-9]+\b/g,
  /\bsk-[A-Za-z0-9_-]{8,}\b/g,
  /\bBearer\s+[A-Za-z0-9._~+/-]+=*/gi,
];
const keyedSecretPattern = /\b(token|secret|password|authorization|api[_-]?key)\s*[:=]\s*([^\s,;]+)/gi;

function redact(value) {
  let result = value;
  for (const pattern of secretPatterns) result = result.replace(pattern, "[REDACTED]");
  return result.replace(keyedSecretPattern, "$1=[REDACTED]");
}

function drainRedacted(stream, file) {
  const output = fs.createWriteStream(file, { flags: "a", mode: 0o600 });
  let pending = "";
  let writable = true;
  output.on("error", () => { writable = false; stream.resume(); });
  stream.setEncoding("utf8");
  const write = (value) => {
    if (value && writable && !output.write(redact(value))) {
      stream.pause();
      output.once("drain", () => stream.resume());
    }
  };
  stream.on("data", (chunk) => {
    pending += chunk;
    const newline = pending.lastIndexOf("\n");
    if (newline >= 0) {
      write(pending.slice(0, newline + 1));
      pending = pending.slice(newline + 1);
    }
    if (pending.length > 65536) {
      write(pending.slice(0, -1024));
      pending = pending.slice(-1024);
    }
  });
  stream.on("end", () => { if (writable) output.end(redact(pending)); });
  stream.on("error", () => { if (writable) output.end(redact(pending)); });
}

module.exports = { drainRedacted, redact };
