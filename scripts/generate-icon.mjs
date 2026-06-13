import sharp from "sharp";
import { resolve, dirname } from "node:path";
import { mkdirSync } from "node:fs";

const args = process.argv.slice(2);
if (args.length < 2) {
  console.error("Usage: node generate-icon.mjs <input.svg> <output.png>");
  process.exit(1);
}

const [inputSvg, outputPng] = args.map((p) => resolve(p));

mkdirSync(dirname(outputPng), { recursive: true });

await sharp(inputSvg)
  .resize(1024, 1024, { fit: "contain", background: { r: 0, g: 0, b: 0, alpha: 0 } })
  .png()
  .toFile(outputPng);

console.log(`Icon written: ${outputPng}`);
