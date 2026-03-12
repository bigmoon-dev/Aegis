// Demo MCP Server for Aegis — zero dependencies, pure Node.js
// Implements MCP protocol (JSON-RPC 2.0 over HTTP) with 5 mock tools.

import { createServer } from "node:http";

const PORT = 9100;

// In-memory post store
let posts = [
  { id: 1, title: "Welcome", content: "Hello from demo server", status: "published" },
  { id: 2, title: "Getting Started", content: "Learn how to use Aegis MCP", status: "draft" },
];
let nextPostId = 3;

// Tool definitions
const TOOLS = [
  {
    name: "echo",
    description: "Echo back the given message. Useful for testing basic connectivity.",
    inputSchema: {
      type: "object",
      properties: {
        message: { type: "string", description: "The message to echo back" },
      },
      required: ["message"],
    },
  },
  {
    name: "get_weather",
    description: "Get simulated weather for a city.",
    inputSchema: {
      type: "object",
      properties: {
        city: { type: "string", description: "City name" },
      },
      required: ["city"],
    },
  },
  {
    name: "list_posts",
    description: "List all posts in the system.",
    inputSchema: {
      type: "object",
      properties: {},
    },
  },
  {
    name: "publish_post",
    description: "Publish a new post (destructive operation).",
    inputSchema: {
      type: "object",
      properties: {
        title: { type: "string", description: "Post title" },
        content: { type: "string", description: "Post content" },
      },
      required: ["title", "content"],
    },
  },
  {
    name: "admin_reset",
    description: "Reset all data to defaults (admin only).",
    inputSchema: {
      type: "object",
      properties: {},
    },
  },
];

// Weather data for simulation
const WEATHER = {
  Tokyo: { temp: 18, condition: "Partly Cloudy", humidity: 65 },
  London: { temp: 12, condition: "Rainy", humidity: 82 },
  "New York": { temp: 22, condition: "Sunny", humidity: 45 },
  Paris: { temp: 15, condition: "Overcast", humidity: 70 },
};

function handleToolCall(name, args) {
  switch (name) {
    case "echo":
      return { text: args.message || "(empty)" };

    case "get_weather": {
      const city = args.city || "Unknown";
      const w = WEATHER[city] || { temp: Math.floor(Math.random() * 30) + 5, condition: "Clear", humidity: Math.floor(Math.random() * 60) + 30 };
      return { text: `Weather in ${city}: ${w.temp}°C, ${w.condition}, Humidity: ${w.humidity}%` };
    }

    case "list_posts":
      return { text: JSON.stringify(posts, null, 2) };

    case "publish_post": {
      const post = { id: nextPostId++, title: args.title, content: args.content, status: "published" };
      posts.push(post);
      return { text: `Post #${post.id} "${post.title}" published successfully.` };
    }

    case "admin_reset":
      posts = [
        { id: 1, title: "Welcome", content: "Hello from demo server", status: "published" },
        { id: 2, title: "Getting Started", content: "Learn how to use Aegis MCP", status: "draft" },
      ];
      nextPostId = 3;
      return { text: "All data reset to defaults." };

    default:
      return null;
  }
}

function handleJsonRpc(req) {
  const { method, params, id } = req;

  switch (method) {
    case "initialize":
      return {
        jsonrpc: "2.0",
        id,
        result: {
          protocolVersion: "2024-11-05",
          capabilities: { tools: {} },
          serverInfo: { name: "aegis-demo-server", version: "1.0.0" },
        },
      };

    case "notifications/initialized":
      // Notification — no response
      return null;

    case "ping":
      return { jsonrpc: "2.0", id, result: {} };

    case "tools/list":
      return { jsonrpc: "2.0", id, result: { tools: TOOLS } };

    case "tools/call": {
      const toolName = params?.name;
      const args = params?.arguments || {};
      const result = handleToolCall(toolName, args);
      if (result === null) {
        return {
          jsonrpc: "2.0",
          id,
          error: { code: -32601, message: `Unknown tool: ${toolName}` },
        };
      }
      return {
        jsonrpc: "2.0",
        id,
        result: { content: [{ type: "text", text: result.text }] },
      };
    }

    default:
      return {
        jsonrpc: "2.0",
        id,
        error: { code: -32601, message: `Method not found: ${method}` },
      };
  }
}

const server = createServer((req, res) => {
  // Health check
  if (req.method === "GET" && req.url === "/health") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ status: "ok" }));
    return;
  }

  // MCP endpoint
  if (req.method === "POST" && req.url === "/mcp") {
    let body = "";
    req.on("data", (chunk) => (body += chunk));
    req.on("end", () => {
      try {
        const rpcReq = JSON.parse(body);
        const rpcResp = handleJsonRpc(rpcReq);
        if (rpcResp === null) {
          // Notification — return 202 with no body
          res.writeHead(202);
          res.end();
          return;
        }
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify(rpcResp));
      } catch (e) {
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({
          jsonrpc: "2.0",
          id: null,
          error: { code: -32700, message: "Parse error" },
        }));
      }
    });
    return;
  }

  res.writeHead(404);
  res.end("Not Found");
});

server.listen(PORT, "127.0.0.1", () => {
  console.log(`Demo MCP server listening on 127.0.0.1:${PORT}`);
});
