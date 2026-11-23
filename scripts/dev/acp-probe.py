#!/usr/bin/env python3
"""Check an ACP agent (design note 053) against what Forgeline's client assumes, with one real model call.

    python3 scripts/dev/acp-probe.py [--mcp http|stdio] [--] copilot --acp

It starts the agent, gives it a one-tool MCP server (HTTP on 127.0.0.1, or stdio) in session/new, asks it to call that tool,
allows its permission requests only for MCP tools (kind "other") and rejects everything else, and prints what it sent. It exits 0 only if the agent called the tool and
the answer reached it. This costs one model request on the agent's own account; it touches nothing else. Found with it
(Copilot CLI 1.0.107, 2026-11-23): Copilot ignores a stdio MCP server and takes an HTTP one, and asks permission before
every tool, Forgeline's own included.
"""
import json, select, subprocess, sys, tempfile, threading, time
from http.server import BaseHTTPRequestHandler, HTTPServer

args = sys.argv[1:]
transport = "http"
if args[:1] == ["--mcp"]:
    transport, args = args[1], args[2:]
if args[:1] == ["--"]:
    args = args[1:]
agent = args or ["copilot", "--acp"]
TOOL, REPLY = "list_pipelines", "pipelines: ship-agent (2 steps), nightly-build (3 steps)"
seen = {"mcp_requests": []}


def mcp_answer(m):
    i, method = m.get("id"), m.get("method")
    seen["mcp_requests"].append(method)
    res = None
    if method == "initialize":
        res = {"protocolVersion": (m.get("params") or {}).get("protocolVersion", "2025-03-26"), "capabilities": {"tools": {}}, "serverInfo": {"name": "probe", "version": "1"}}
    elif method == "tools/list":
        res = {"tools": [{"name": TOOL, "description": "List the saved pipelines.", "inputSchema": {"type": "object", "properties": {}}}]}
    elif method == "tools/call":
        res = {"content": [{"type": "text", "text": REPLY}]}
    if i is None:
        return None
    return {"jsonrpc": "2.0", "id": i, **({"result": res} if res is not None else {"error": {"code": -32601, "message": "no"}})}


class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_POST(self):
        m = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
        out = mcp_answer(m)
        if out is None:
            self.send_response(202); self.end_headers(); return
        b = json.dumps(out).encode()
        self.send_response(200); self.send_header("Content-Type", "application/json"); self.send_header("Content-Length", str(len(b))); self.end_headers(); self.wfile.write(b)


work = tempfile.mkdtemp(prefix="acp-probe-")            # the agent's working directory: nothing of ours in it
aside = tempfile.mkdtemp(prefix="acp-probe-server-")   # where the stdio stub lives, out of the agent's sight
if transport == "http":
    srv = HTTPServer(("127.0.0.1", 0), H)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    mcp = [{"type": "http", "name": "forgeline", "url": "http://127.0.0.1:%d/mcp/engine/probe" % srv.server_port, "headers": []}]
else:
    stub = aside + "/stdio_mcp.py"
    open(stub, "w").write("import sys, json\nfor l in sys.stdin:\n    m = json.loads(l)\n    open(%r, 'a').write(str(m.get('method')) + '\\n')\n" % (aside + "/stdio.log")
                          + "    i = m.get('id')\n    if i is None: continue\n    r = {'initialize': {'protocolVersion': '2025-03-26', 'capabilities': {'tools': {}}, 'serverInfo': {'name': 'p', 'version': '1'}},\n"
                          + "         'tools/list': {'tools': [{'name': %r, 'description': 'List pipelines.', 'inputSchema': {'type': 'object', 'properties': {}}}]},\n" % TOOL
                          + "         'tools/call': {'content': [{'type': 'text', 'text': %r}]}}.get(m['method'])\n" % REPLY
                          + "    print(json.dumps({'jsonrpc': '2.0', 'id': i, 'result': r}), flush=True)\n")
    mcp = [{"name": "forgeline", "command": sys.executable, "args": ["-I", stub], "env": []}]

p = subprocess.Popen(agent, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, cwd=work)
events, answer = [], []


def send(m): p.stdin.write(json.dumps(m) + "\n"); p.stdin.flush()


def until(i, timeout):
    end = time.time() + timeout
    while time.time() < end:
        if not select.select([p.stdout], [], [], 1)[0]:
            continue
        line = p.stdout.readline()
        if not line:
            return None
        m = json.loads(line)
        if "method" in m and "id" in m:
            events.append("agent asks " + m["method"] + " " + json.dumps(m.get("params"))[:160])
            if m["method"] == "session/request_permission":
                opts = m["params"].get("options", [])
                # Only an MCP tool (kind "other") is allowed: an agent that cannot see our tool may try to do the job with a
                # shell command, and that must not count as the tool having been called.
                kind = "allow_once" if (m["params"].get("toolCall") or {}).get("kind") == "other" else "reject_once"
                pick = next((o["optionId"] for o in opts if o.get("kind") == kind), None)
                events.append("  -> %s (%s)" % (kind, (m["params"].get("toolCall") or {}).get("kind")))
                send({"jsonrpc": "2.0", "id": m["id"], "result": {"outcome": {"outcome": "selected", "optionId": pick} if pick else {"outcome": "cancelled"}}})
            else:
                send({"jsonrpc": "2.0", "id": m["id"], "error": {"code": -32601, "message": "not provided"}})
        elif "method" in m:
            u = (m.get("params") or {}).get("update", {})
            kind = u.get("sessionUpdate")
            if kind == "agent_message_chunk":
                answer.append(u.get("content", {}).get("text", ""))
            elif kind in ("tool_call", "tool_call_update", "plan"):
                events.append("update %s %s" % (kind, json.dumps({k: u.get(k) for k in ("toolCallId", "title", "kind", "status")})))
        elif m.get("id") == i:
            return m


send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": 1, "clientCapabilities": {"fs": {"readTextFile": False, "writeTextFile": False}, "terminal": False}, "clientInfo": {"name": "forgeline", "title": "Forgeline", "version": "1"}}})
init = until(1, 30)
caps = (init or {}).get("result", {}).get("agentCapabilities", {})
print("agent:", (init or {}).get("result", {}).get("agentInfo"), "mcpCapabilities:", caps.get("mcpCapabilities"))
send({"jsonrpc": "2.0", "id": 2, "method": "session/new", "params": {"cwd": work, "mcpServers": mcp}})
new = until(2, 60)
if not new or "result" not in new:
    print("session/new failed:", new); p.kill(); sys.exit(1)
sid = new["result"]["sessionId"]
send({"jsonrpc": "2.0", "id": 3, "method": "session/prompt", "params": {"sessionId": sid, "prompt": [{"type": "text", "text": "Call your forgeline %s tool and tell me in one sentence which pipelines exist." % TOOL}]}})
res = until(3, 180)
p.terminate()
print("\n".join(events))
print("answer:", "".join(answer).strip())
called = "tools/call" in seen["mcp_requests"] if transport == "http" else (open(aside + "/stdio.log").read().count("tools/call") > 0 if __import__("os").path.exists(aside + "/stdio.log") else False)
print("tool server asked (%s):" % transport, "yes, it called the tool" if called else "NO: the agent never called the tool (it may ignore %s MCP servers)" % transport)
sys.exit(0 if called and res and "result" in res and "ship-agent" in "".join(answer) else 1)
