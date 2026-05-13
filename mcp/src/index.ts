#!/usr/bin/env node

import dotenv from "dotenv";

import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";

import { z } from "zod";
import { Client as PgClient } from "pg";
import { spawn } from "node:child_process";
import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { resolve } from "node:path";

function getGitRepoRoot(): string {
  try {
    const out = execFileSync("git", ["rev-parse", "--show-toplevel"], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();

    if (!out) throw new Error("Empty git toplevel");
    const p = resolve(out);
    if (!existsSync(p)) throw new Error(`git toplevel does not exist: ${p}`);
    return p;
  } catch {
    // Fallback: run relative to wherever Cursor launched us.
    // (Useful when not in a git worktree, or git isn't on PATH.)
    return process.cwd();
  }
}

const repoRoot = getGitRepoRoot();

dotenv.config({ path: resolve(repoRoot, "mcp", ".env") });

const Env = z
  .object({
    // Optional: enable DB tool if set
    ARRFLIX_DATABASE_URL: z.string().optional(),
  })
  .parse(process.env);

async function dockerComposeLogs(
  service: string,
  lines = 200
): Promise<string> {
  const args = [
    "compose",
    "logs",
    "--no-color",
    "--tail",
    String(lines),
    service,
  ];

  return await new Promise<string>((resolvePromise, rejectPromise) => {
    const child = spawn("docker", args, { stdio: ["ignore", "pipe", "pipe"] });

    let out = "";
    let err = "";
    child.stdout.on("data", (d) => (out += d.toString("utf8")));
    child.stderr.on("data", (d) => (err += d.toString("utf8")));

    child.on("error", rejectPromise);
    child.on("close", (code) => {
      if (code === 0) return resolvePromise(out);
      rejectPromise(
        new Error(`docker compose logs failed (code ${code}): ${err}`)
      );
    });
  });
}

function isReadOnlySql(sql: string): boolean {
  const s = sql.trim().toLowerCase();
  return s.startsWith("select") || s.startsWith("with");
}

async function pgQuery(sql: string, params: unknown[] = []) {
  if (!Env.ARRFLIX_DATABASE_URL) {
    throw new Error("ARRFLIX_DATABASE_URL is not set.");
  }
  if (!isReadOnlySql(sql)) {
    throw new Error(
      "Only read-only queries are allowed (SELECT / WITH ... SELECT)."
    );
  }

  const client = new PgClient({ connectionString: Env.ARRFLIX_DATABASE_URL });
  await client.connect();
  try {
    return await client.query(sql, params);
  } finally {
    await client.end();
  }
}

const server = new Server(
  { name: "arrflix-mcp", version: "0.1.0" },
  { capabilities: { tools: {} } }
);

server.setRequestHandler(ListToolsRequestSchema, async () => {
  return {
    tools: [
      {
        name: "arrflix_docker_logs",
        description: "Get recent docker compose logs for a service.",
        inputSchema: {
          type: "object",
          properties: {
            service: { type: "string", description: "Compose service name" },
            lines: {
              type: "number",
              description: "Tail N lines (1-2000). Default 200.",
            },
          },
          required: ["service"],
        },
      },
      {
        name: "arrflix_db_query",
        description:
          "Run a READ-ONLY Postgres query (SELECT/CTE only) against ARRFLIX_DATABASE_URL. Returns JSON rows.",
        inputSchema: {
          type: "object",
          properties: {
            sql: { type: "string", description: "SQL query (SELECT/CTE only)" },
            params: {
              type: "array",
              items: {},
              description: "Optional parameter array for $1, $2, ...",
            },
            maxRows: {
              type: "number",
              description: "Max rows to return (1-500). Default 200.",
            },
          },
          required: ["sql"],
        },
      },
    ],
  };
});

server.setRequestHandler(CallToolRequestSchema, async (request) => {
  const name = request.params.name;
  const args = (request.params.arguments ?? {}) as Record<string, unknown>;

  try {
    switch (name) {
      case "arrflix_docker_logs": {
        const service = z.string().min(1).parse(args.service);
        const lines = z
          .number()
          .int()
          .min(1)
          .max(2000)
          .optional()
          .parse(args.lines);

        const out = await dockerComposeLogs(service, lines ?? 200);
        return { content: [{ type: "text", text: out || "(no output)" }] };
      }

      case "arrflix_db_query": {
        const sql = z.string().min(1).parse(args.sql);
        const params = z.array(z.any()).optional().parse(args.params) ?? [];
        const maxRows = z
          .number()
          .int()
          .min(1)
          .max(500)
          .optional()
          .parse(args.maxRows);

        const res = await pgQuery(sql, params);
        const rows = res.rows.slice(0, maxRows ?? 200);

        return {
          content: [
            {
              type: "text",
              text: JSON.stringify({ rowCount: res.rowCount, rows }, null, 2),
            },
          ],
        };
      }

      default:
        throw new Error(`Unknown tool: ${name}`);
    }
  } catch (e: any) {
    return {
      content: [{ type: "text", text: `Error: ${e?.message ?? String(e)}` }],
      isError: true,
    };
  }
});

async function main() {
  console.log("Starting arrflix-mcp server...");
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((error) => {
  // Keep JSON-RPC clean; errors to stderr only.
  console.error("Server error:", error);
  process.exit(1);
});
