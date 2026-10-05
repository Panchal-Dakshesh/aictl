#!/usr/bin/env python3
"""Builds testdata/home: a fake $HOME with one made-up session per tool.
Nothing here comes from a real conversation.

Dot-directories are stored as dot-<name> (dot-claude, dot-codex, dot-gemini)
so editors and sync tools don't treat them as real tool config; tests copy
the tree to a temp dir and rename them back."""
import json, os, sqlite3, shutil

ROOT = os.path.join(os.path.dirname(__file__), "..", "testdata", "home")
shutil.rmtree(ROOT, ignore_errors=True)

def w(path, data):
    path = os.path.join(ROOT, path)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(data)
    return path

def jl(rows): return "\n".join(json.dumps(r) for r in rows) + "\n"

# ---- Claude Code
sid = "11111111-aaaa-4bbb-8ccc-000000000001"
cwd = "/Users/demo/Work/shop"
base = dict(sessionId=sid, cwd=cwd, gitBranch="main", version="2.0.0")
w(f"dot-claude/projects/-Users-demo-Work-shop/{sid}.jsonl", jl([
    dict(type="summary", summary="Fix flaky checkout test", leafUuid="x"),
    dict(base, type="user", timestamp="2026-10-01T09:00:00.000Z", message=dict(role="user", content="<command-name>/clear</command-name>"), isMeta=True),
    dict(base, type="user", timestamp="2026-10-01T09:00:05.000Z", message=dict(role="user", content="the checkout test fails 1 in 5 runs, why?")),
    dict(base, type="assistant", timestamp="2026-10-01T09:00:20.000Z", message=dict(role="assistant", model="claude-sonnet-4", content=[
        dict(type="text", text="It depends on wall-clock time. Freeze the clock in the test."),
        dict(type="tool_use", name="Read", input=dict(file_path="spec/checkout_spec.rb"))])),
    dict(base, type="user", timestamp="2026-10-01T09:01:00.000Z", message=dict(role="user", content=[dict(type="tool_result", content="ok")])),
    dict(base, type="user", timestamp="2026-10-01T09:02:00.000Z", message=dict(role="user", content="do it. my key is sk-ant-abcdefghijklmnopqrstuvwxyz123456")),
    dict(base, type="assistant", timestamp="2026-10-01T09:03:00.000Z", message=dict(role="assistant", model="claude-sonnet-4", content=[dict(type="text", text="Done: wrapped the spec in travel_to.")])),
]))
w("dot-claude/projects/-Users-demo-Work-shop/agent-ignored.jsonl", jl([dict(base, type="user", isSidechain=True, message=dict(role="user", content="sub"))]))

# ---- Codex (current envelope format)
cid = "22222222-bbbb-4ccc-8ddd-000000000002"
w(f"dot-codex/sessions/2026/10/02/rollout-2026-10-02T10-00-00-{cid}.jsonl", jl([
    dict(timestamp="2026-10-02T10:00:00.000Z", type="session_meta", payload=dict(id=cid, timestamp="2026-10-02T10:00:00.000Z", cwd="/Users/demo/Work/pipeline", git=dict(branch="feat/cdc"))),
    dict(timestamp="2026-10-02T10:00:01.000Z", type="response_item", payload=dict(type="message", role="user", content=[dict(type="input_text", text="<environment_context>cwd</environment_context>")])),
    dict(timestamp="2026-10-02T10:00:02.000Z", type="turn_context", payload=dict(cwd="/Users/demo/Work/pipeline", model="gpt-5-codex")),
    dict(timestamp="2026-10-02T10:00:03.000Z", type="response_item", payload=dict(type="message", role="user", content=[dict(type="input_text", text="add a dead letter topic to the kafka consumer")])),
    dict(timestamp="2026-10-02T10:00:09.000Z", type="response_item", payload=dict(type="function_call", name="shell", arguments="{}")),
    dict(timestamp="2026-10-02T10:00:30.000Z", type="response_item", payload=dict(type="message", role="assistant", content=[dict(type="output_text", text="Added orders.dlq and a retry policy.")])),
]))
# legacy flat format
lid = "33333333-cccc-4ddd-8eee-000000000003"
w(f"dot-codex/sessions/rollout-2025-05-01-{lid}.jsonl", jl([
    dict(id=lid, timestamp="2025-05-01T08:00:00.000Z", instructions=""),
    dict(type="message", role="user", content=[dict(type="input_text", text="explain this regex")]),
    dict(type="message", role="assistant", content=[dict(type="output_text", text="It matches ISO dates.")]),
]))

# ---- Gemini CLI
gid = "44444444-dddd-4eee-8fff-000000000004"
w("dot-gemini/tmp/5f3a/.project_root", "/Users/demo/Work/site\n")
w("dot-gemini/tmp/5f3a/chats/session-2026-09-30T12-00-4444.json", json.dumps(dict(
    sessionId=gid, projectHash="5f3a", startTime="2026-09-30T12:00:00.000Z", lastUpdated="2026-09-30T12:05:00.000Z",
    messages=[
        dict(id="1", timestamp="2026-09-30T12:00:00.000Z", type="user", content="make the hero section lighter"),
        dict(id="2", timestamp="2026-09-30T12:01:00.000Z", type="gemini", model="gemini-2.5-pro", content="Switched the background to #ffffff.", toolCalls=[dict(name="replace")]),
    ])))

# ---- Antigravity (SQLite + protobuf)
def varint(n):
    out = b""
    while True:
        b = n & 0x7f; n >>= 7
        out += bytes([b | (0x80 if n else 0)])
        if not n: return out
def fld(num, val):
    if isinstance(val, int): return varint(num << 3) + varint(val)
    if isinstance(val, str): val = val.encode()
    return varint(num << 3 | 2) + varint(len(val)) + val
def ts(sec): return fld(1, fld(1, sec))  # field 5.1.1
aid = "55555555-eeee-4fff-8aaa-000000000005"
db = os.path.join(ROOT, f"dot-gemini/antigravity/conversations/{aid}.db")
os.makedirs(os.path.dirname(db), exist_ok=True)
c = sqlite3.connect(db)
c.execute("create table steps (idx integer primary key, step_type integer, step_payload blob)")
c.execute("create table trajectory_metadata_blob (id text primary key, data blob)")
c.execute("insert into trajectory_metadata_blob values ('main', ?)", (fld(3, "traj") + fld(6, aid) + fld(18, "/Users/demo/Work/jarvis"),))
c.execute("insert into steps values (0, 14, ?)", (fld(5, ts(1790000000)) + fld(19, fld(2, "build a voice assistant backend")),))
c.execute("insert into steps values (1, 21, ?)", (fld(5, ts(1790000005) + fld(4, fld(2, "run_command"))),))
c.execute("insert into steps values (2, 15, ?)", (fld(5, ts(1790000010)) + fld(20, fld(1, "Scaffolded a FastAPI server with a /speak route.")),))
c.commit(); c.close()
sub = os.path.join(ROOT, "dot-gemini/antigravity/conversations/66666666-sub.db")
c = sqlite3.connect(sub)
c.execute("create table steps (idx integer primary key, step_type integer, step_payload blob)")
c.execute("create table trajectory_metadata_blob (id text primary key, data blob)")
c.execute("insert into trajectory_metadata_blob values ('main', ?)", (fld(5, aid),))
c.commit(); c.close()
with open(os.path.join(ROOT, "dot-gemini/antigravity/agyhub_summaries_proto.pb"), "wb") as f:
    f.write(fld(1, fld(1, aid) + fld(2, fld(1, "Voice assistant backend") + fld(3, fld(1, 1790000020)))))

# ---- Cursor desktop
ud = os.path.join(ROOT, "Library/Application Support/Cursor/User")
os.makedirs(os.path.join(ud, "globalStorage"), exist_ok=True)
c = sqlite3.connect(os.path.join(ud, "globalStorage/state.vscdb"))
c.execute("create table cursorDiskKV (key text unique on conflict replace, value blob)")
comp = "77777777-ffff-4aaa-8bbb-000000000007"
c.execute("insert into cursorDiskKV values (?, ?)", ("composerData:" + comp, json.dumps(dict(
    composerId=comp, name="Landing page copy", createdAt=1790100000000, lastUpdatedAt=1790100600000,
    fullConversationHeadersOnly=[dict(bubbleId="b1", type=1), dict(bubbleId="b2", type=2)]))))
c.execute("insert into cursorDiskKV values (?, ?)", (f"bubbleId:{comp}:b1", json.dumps(dict(bubbleId="b1", type=1, text="rewrite the hero headline"))))
c.execute("insert into cursorDiskKV values (?, ?)", (f"bubbleId:{comp}:b2", json.dumps(dict(bubbleId="b2", type=2, text="Try: Ship data, not dashboards."))))
c.commit(); c.close()
ws = os.path.join(ud, "workspaceStorage/abc123")
os.makedirs(ws, exist_ok=True)
with open(os.path.join(ws, "workspace.json"), "w") as f:
    json.dump(dict(folder="file:///Users/demo/Work/landing"), f)
c = sqlite3.connect(os.path.join(ws, "state.vscdb"))
c.execute("create table ItemTable (key text unique on conflict replace, value blob)")
c.execute("insert into ItemTable values ('composer.composerData', ?)", (json.dumps(dict(allComposers=[dict(composerId=comp)])),))
c.commit(); c.close()

# ---- memory files
w("dot-claude/CLAUDE.md", "# Global\n\n- Never add co-author lines to commits.\n")
print("fixtures written to", os.path.normpath(ROOT))
