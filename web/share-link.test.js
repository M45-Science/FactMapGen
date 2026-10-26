const test = require("node:test");
const assert = require("node:assert/strict");
const { deflateSync, inflateSync } = require("node:zlib");
const share = require("./share-link.js");

test("share URL preserves the exact compressed exchange bytes and view", () => {
  const raw = Buffer.from(Array.from({ length: 256 }, (_, i) => i));
  const compressed = deflateSync(raw).toString("base64");
  const exchange = `>>>${compressed}<<<`;
  for (const planet of ["nauvis", "vulcanus", "gleba", "fulgora", "aquilo"]) {
    const view = { planet, centerX: -1536.5, centerY: 2048, scale: 0.125 };
    const url = new URL(share.create("https://mapgen.example/?old=1#old", exchange, view));
    const decoded = share.parse(url.hash);
    assert.deepEqual(decoded, { exchangeString: exchange, ...view });
    assert.deepEqual(inflateSync(Buffer.from(decoded.exchangeString.slice(3, -3), "base64")), raw);
    assert.equal(url.search, "");
    assert.doesNotMatch(url.hash, /[%+/=]{2}|%/);
  }
});

test("default view adds no extra URL fields and restores base64 padding", () => {
  for (const exchange of [">>>YQ==<<<", ">>>YWI=<<<", ">>>YWJj<<<"]) {
    const url = new URL(share.create("https://example.com/", exchange));
    assert.equal(new URLSearchParams(url.hash.slice(1)).size, 1);
    assert.deepEqual(share.parse(url.hash), { exchangeString: exchange, planet: "nauvis", centerX: 0, centerY: 0, scale: 1 });
  }
  assert.equal(share.parse("#unrelated"), null);
});

test("malformed payloads and out-of-range views are rejected", () => {
  for (const hash of ["#m=", "#m=a", "#m=a%2Bb", "#m=YQ&p=bad", "#m=YQ&x=Infinity", "#m=YQ&y=1000000001", "#m=YQ&z=0", "#m=YQ&x=", "#m=" + "a".repeat(512000)]) {
    assert.throws(() => share.parse(hash));
  }
  assert.throws(() => share.create("https://example.com/", "not an exchange string"));
});
