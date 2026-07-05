#!/usr/bin/env node

import dotenv from "dotenv";

import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";

import { z } from "zod";
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

// The DB tool execs `psql` inside the compose service and connects over the
// container's local unix socket (trust auth), so it needs no host/port/password
// — only the role and database, which default to the dev seed values. All three
// have defaults, so mcp/.env is optional.
const Env = z
  .object({
    ARRFLIX_DB_SERVICE: z.string().default("arrflix-dev"),
    ARRFLIX_DB_USER: z.string().default("arrflix"),
    ARRFLIX_DB_NAME: z.string().default("arrflix"),
  })
  .parse(process.env);

// Run `docker compose <args>` from the repo root so compose finds the project's
// docker-compose.yml regardless of where the client launched us.
async function dockerCompose(args: string[]): Promise<string> {
  return await new Promise<string>((resolvePromise, rejectPromise) => {
    const child = spawn("docker", ["compose", ...args], {
      cwd: repoRoot,
      stdio: ["ignore", "pipe", "pipe"],
    });

    let out = "";
    let err = "";
    child.stdout.on("data", (d) => (out += d.toString("utf8")));
    child.stderr.on("data", (d) => (err += d.toString("utf8")));

    child.on("error", rejectPromise);
    child.on("close", (code) => {
      if (code === 0) return resolvePromise(out);
      rejectPromise(
        new Error(`docker compose ${args[0]} failed (code ${code}): ${err}`)
      );
    });
  });
}

async function dockerComposeLogs(
  service: string,
  lines = 200
): Promise<string> {
  return await dockerCompose([
    "logs",
    "--no-color",
    "--tail",
    String(lines),
    service,
  ]);
}

function isReadOnlySql(sql: string): boolean {
  const s = sql.trim().toLowerCase();
  return s.startsWith("select") || s.startsWith("with");
}

// Execute a read-only query by running psql *inside* the DB container via
// `docker compose exec`, connecting over the container's local unix socket — no
// host port mapping needed. Wrapping the query in json_agg yields a single JSON
// document we can parse directly out of psql's tuples-only output.
async function pgQuery(sql: string): Promise<unknown[]> {
  if (!isReadOnlySql(sql)) {
    throw new Error(
      "Only read-only queries are allowed (SELECT / WITH ... SELECT)."
    );
  }

  const wrapped = `select coalesce(json_agg(t), '[]') from (${sql}) t`;
  const out = await dockerCompose([
    "exec",
    "-T", // no TTY: we're spawned without one
    Env.ARRFLIX_DB_SERVICE,
    "psql",
    "-U",
    Env.ARRFLIX_DB_USER,
    "-d",
    Env.ARRFLIX_DB_NAME,
    "-tA", // tuples only, unaligned — clean JSON, no headers/padding
    "-c",
    wrapped,
  ]);

  const text = out.trim();
  if (!text) return [];
  return JSON.parse(text) as unknown[];
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
        const maxRows = z
          .number()
          .int()
          .min(1)
          .max(500)
          .optional()
          .parse(args.maxRows);

        const allRows = await pgQuery(sql);
        const rows = allRows.slice(0, maxRows ?? 200);

        return {
          content: [
            {
              type: "text",
              text: JSON.stringify({ rowCount: rows.length, rows }, null, 2),
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
