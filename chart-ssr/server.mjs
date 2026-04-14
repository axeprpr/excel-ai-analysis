import crypto from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";

import express from "express";
import "ignore-styles";
import { render } from "@antv/gpt-vis-ssr";

const app = express();

const port = Number.parseInt(process.env.PORT || "3001", 10);
const outputDir = process.env.OUTPUT_DIR || "/app/public/images";
const publicBaseUrl = (process.env.PUBLIC_BASE_URL || `http://127.0.0.1:${port}`).replace(/\/$/, "");

async function ensureOutputDir() {
  await fs.mkdir(outputDir, { recursive: true });
}

function buildPublicUrl(filename) {
  return `${publicBaseUrl}/images/${filename}`;
}

function normalizeChartSpec(body) {
  const { serviceId, tool, input, source, ...spec } = body || {};
  if (tool) {
    throw new Error("local gpt-vis-ssr service does not support map tool requests");
  }
  if (!spec.type) {
    throw new Error("missing chart type");
  }
  return spec;
}

app.use(express.json({ limit: "10mb" }));
app.use("/images", express.static(outputDir, { maxAge: "7d" }));

app.get("/healthz", async (_req, res) => {
  await ensureOutputDir();
  res.json({
    status: "ok",
    output_dir: outputDir,
    public_base_url: publicBaseUrl,
  });
});

app.post("/render", async (req, res) => {
  let vis;
  try {
    await ensureOutputDir();
    const spec = normalizeChartSpec(req.body);
    vis = await render(spec);
    const buffer = vis.toBuffer();
    const filename = `${Date.now()}-${crypto.randomUUID()}.png`;
    const filePath = path.join(outputDir, filename);
    await fs.writeFile(filePath, buffer);
    res.json({
      success: true,
      resultObj: buildPublicUrl(filename),
    });
  } catch (error) {
    res.status(500).json({
      success: false,
      errorMessage: error instanceof Error ? error.message : String(error),
    });
  } finally {
    if (vis && typeof vis.destroy === "function") {
      try {
        vis.destroy();
      } catch {
        // ignore cleanup errors
      }
    }
  }
});

app.listen(port, "0.0.0.0", async () => {
  await ensureOutputDir();
  console.log(`chart-ssr listening on http://0.0.0.0:${port}`);
});
