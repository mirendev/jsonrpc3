#!/usr/bin/env node
/**
 * Build script for browser-compatible bundles
 * Generates ESM, IIFE, and UMD formats
 */

import * as esbuild from "esbuild";
import { execSync } from "child_process";
import { rmSync, mkdirSync } from "fs";

const entryPoint = "src/browser.ts";
const outdir = "dist";

// Clean dist directory
console.log("Cleaning dist directory...");
rmSync(outdir, { recursive: true, force: true });
mkdirSync(outdir, { recursive: true });

// Shared build options
const sharedOptions: esbuild.BuildOptions = {
  entryPoints: [entryPoint],
  bundle: true,
  minify: true,
  sourcemap: true,
  target: ["es2020"],
  // Bundle nanoid since it's small and we want self-contained browser builds
  external: [],
};

// Build ESM (for modern bundlers)
console.log("Building ESM format...");
await esbuild.build({
  ...sharedOptions,
  format: "esm",
  outfile: `${outdir}/browser.esm.js`,
  splitting: false,
});

// Build IIFE (for direct browser use via script tag)
console.log("Building IIFE format...");
await esbuild.build({
  ...sharedOptions,
  format: "iife",
  globalName: "JSONRPC3",
  outfile: `${outdir}/browser.iife.js`,
});

// Build UMD (universal - works in browser, AMD, CommonJS)
console.log("Building UMD format...");
// esbuild doesn't have native UMD support, so we'll create a wrapper
await esbuild.build({
  ...sharedOptions,
  format: "cjs",
  outfile: `${outdir}/browser.umd.js`,
  footer: {
    js: `
// UMD wrapper
(function (root, factory) {
  if (typeof define === 'function' && define.amd) {
    // AMD
    define([], factory);
  } else if (typeof module === 'object' && module.exports) {
    // CommonJS
    module.exports = factory();
  } else {
    // Browser globals
    root.JSONRPC3 = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  return exports;
}));
`,
  },
});

// Generate TypeScript declarations
console.log("Generating TypeScript declarations...");
try {
  execSync("tsc --emitDeclarationOnly --declaration --outDir dist src/browser.ts", {
    stdio: "inherit",
  });
} catch (error) {
  console.error("Warning: TypeScript declaration generation failed");
  console.error(error);
}

console.log("Build complete!");
console.log(`  ESM:  ${outdir}/browser.esm.js`);
console.log(`  IIFE: ${outdir}/browser.iife.js`);
console.log(`  UMD:  ${outdir}/browser.umd.js`);
console.log(`  Types: ${outdir}/browser.d.ts`);
