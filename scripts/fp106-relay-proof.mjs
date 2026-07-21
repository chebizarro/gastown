#!/usr/bin/env node
import WebSocket from "ws";
import {
  finalizeEvent,
  generateSecretKey,
  getPublicKey,
  verifyEvent,
} from "nostr-tools";

const relay = process.env.FP106_RELAY || "wss://relay.sharegap.net";
const timeoutMs = Number(process.env.FP106_TIMEOUT_MS || 8000);
const secretKey = generateSecretKey();
const pubkey = getPublicKey(secretKey);
const now = Math.floor(Date.now() / 1000);
const suffix = `${now}-${Math.random().toString(16).slice(2, 10)}`;
const scope = `fp-106-proof-${suffix}`;
const taskID = `${scope}-task`;
const taskD = `task:${taskID}`;
const queueD = `queue:${scope}`;
const coordinate = `30900:${pubkey}:${taskD}`;

const requiredTags = {
  30315: ["d"],
  30316: ["d", "status", "agent"],
  30317: ["d", "agent", "cap"],
  30900: ["d", "domain", "schema"],
  30000: ["d"],
  25910: ["p"],
};

function tagsHave(event, names) {
  return names.every((name) => event.tags.some((tag) => tag[0] === name && tag[1]));
}

function sign(event) {
  return finalizeEvent(event, secretKey);
}

function buildEvents() {
  return [
    sign({
      kind: 30315,
      created_at: now,
      tags: [
        ["d", `${scope}:status`],
        ["gt", "1"],
        ["rig", "gastown"],
        ["role", "proof"],
        ["agent", "gus"],
        ["type", "fp-106-proof"],
        ["visibility", "audit"],
        ["scope", scope],
      ],
      content: JSON.stringify({
        schema: "gt/log@1",
        type: "fp-106-proof",
        source: "gt",
        payload: { reversible: true, production: false },
      }),
    }),
    sign({
      kind: 30316,
      created_at: now,
      tags: [
        ["d", `${scope}:agent:gus`],
        ["gt", "1"],
        ["rig", "gastown"],
        ["status", "working"],
        ["agent", "gus"],
        ["runtime", "gastown"],
        ["schema", "cascadia.agent.heartbeat.v1"],
        ["scope", scope],
      ],
      content: JSON.stringify({ active_tasks: 1 }),
    }),
    sign({
      kind: 30317,
      created_at: now,
      tags: [
        ["d", `agent:gus:cap:${scope}`],
        ["gt", "1"],
        ["rig", "gastown"],
        ["role", "proof"],
        ["agent", "gus"],
        ["cap", "fp-106-canonical-kind-proof"],
        ["runtime", "gastown"],
        ["schema", "cascadia.agent.capability.v1"],
        ["scope", scope],
      ],
      content: JSON.stringify({
        agent_id: "gus",
        capability: "fp-106-canonical-kind-proof",
      }),
    }),
    sign({
      kind: 30900,
      created_at: now,
      tags: [
        ["d", taskD],
        ["gt", "1"],
        ["domain", "task"],
        ["schema", "cascadia.task-state.v1"],
        ["status", "open"],
        ["priority", "P4"],
        ["scope", scope],
      ],
      content: JSON.stringify({
        id: taskID,
        title: "fp-106 reversible interoperability proof",
        status: "open",
        priority: "P4",
      }),
    }),
    sign({
      kind: 30000,
      created_at: now,
      tags: [
        ["d", queueD],
        ["gt", "1"],
        ["schema", "cascadia.task-queue.v1"],
        ["h", "fleet-dev-proof"],
        ["a", coordinate],
        ["scope", scope],
      ],
      content: JSON.stringify({ id: scope }),
    }),
    sign({
      kind: 25910,
      created_at: now,
      tags: [
        ["p", pubkey],
        ["method", "task/update"],
        ["domain", "task"],
        ["op", "update"],
        ["schema", "contextvm.intent.v1"],
        ["a", coordinate],
        ["scope", scope],
      ],
      content: JSON.stringify({
        jsonrpc: "2.0",
        id: `${scope}:mutation`,
        method: "task/update",
        params: {
          id: taskID,
          status: "in_progress",
          coordinate,
          reversible: true,
          production: false,
        },
      }),
    }),
  ];
}

function openRelay() {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(relay);
    const timer = setTimeout(() => reject(new Error(`timeout connecting to ${relay}`)), timeoutMs);
    ws.once("open", () => {
      clearTimeout(timer);
      resolve(ws);
    });
    ws.once("error", reject);
  });
}

function wait(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function publish(ws, event) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      cleanup();
      reject(new Error(`timeout waiting for OK ${event.id}`));
    }, timeoutMs);
    const onMessage = (data) => {
      let msg;
      try {
        msg = JSON.parse(data.toString());
      } catch {
        return;
      }
      if (msg[0] === "AUTH") {
        const authEvent = sign({
          kind: 22242,
          created_at: Math.floor(Date.now() / 1000),
          tags: [
            ["relay", relay],
            ["challenge", msg[1]],
          ],
          content: "",
        });
        ws.send(JSON.stringify(["AUTH", authEvent]));
        return;
      }
      if (msg[0] === "OK" && msg[1] === event.id) {
        cleanup();
        if (msg[2] === true) {
          resolve(msg[3] || "accepted");
        } else {
          reject(new Error(`relay rejected ${event.id}: ${msg[3] || "no reason"}`));
        }
      }
    };
    const cleanup = () => {
      clearTimeout(timer);
      ws.off("message", onMessage);
    };
    ws.on("message", onMessage);
    ws.send(JSON.stringify(["EVENT", event]));
  });
}

async function query(ids) {
  const ws = await openRelay();
  const found = new Map();
  const sub = `fp106-query-${suffix}`;
  ws.on("message", (data) => {
    let msg;
    try {
      msg = JSON.parse(data.toString());
    } catch {
      return;
    }
    if (msg[0] === "EVENT" && msg[1] === sub) {
      found.set(msg[2].id, msg[2]);
    }
  });
  ws.send(JSON.stringify(["REQ", sub, { ids }]));
  await wait(timeoutMs);
  ws.send(JSON.stringify(["CLOSE", sub]));
  ws.close();
  return found;
}

async function main() {
  const events = buildEvents();
  const expected = new Set(events.map((event) => event.id));
  const liveSeen = new Set();
  const ws = await openRelay();
  const sub = `fp106-live-${suffix}`;

  ws.on("message", (data) => {
    let msg;
    try {
      msg = JSON.parse(data.toString());
    } catch {
      return;
    }
    if (msg[0] === "EVENT" && msg[1] === sub && expected.has(msg[2].id)) {
      liveSeen.add(msg[2].id);
    }
  });

  ws.send(JSON.stringify(["REQ", sub, { authors: [pubkey], kinds: [30315, 30316, 30317, 30900, 30000, 25910], "#scope": [scope], since: now - 5 }]));
  await wait(500);

  const publishResults = [];
  for (const event of events) {
    if (!verifyEvent(event)) {
      throw new Error(`local signature verification failed for ${event.kind}`);
    }
    if (!tagsHave(event, requiredTags[event.kind] || [])) {
      throw new Error(`missing required tags for ${event.kind}`);
    }
    publishResults.push({ kind: event.kind, id: event.id, ok: await publish(ws, event) });
  }

  await wait(timeoutMs);
  ws.send(JSON.stringify(["CLOSE", sub]));

  const storedIds = events.filter((event) => event.kind !== 25910).map((event) => event.id);
  const stored = await query(storedIds);
  const deleteEvent = sign({
    kind: 5,
    created_at: Math.floor(Date.now() / 1000),
    tags: [
      ...events.map((event) => ["e", event.id]),
      ["a", `${30315}:${pubkey}:${scope}:status`],
      ["a", `${30316}:${pubkey}:${scope}:agent:gus`],
      ["a", `${30317}:${pubkey}:agent:gus:cap:${scope}`],
      ["a", `${30900}:${pubkey}:${taskD}`],
      ["a", `${30000}:${pubkey}:${queueD}`],
      ["scope", scope],
    ],
    content: "Rollback request for fp-106 reversible non-production relay proof.",
  });
  const deleteResult = await publish(ws, deleteEvent);
  ws.close();

  const queue = events.find((event) => event.kind === 30000);
  const mutation = events.find((event) => event.kind === 25910);
  const mutationContent = JSON.parse(mutation.content);

  console.log(`relay: ${relay}`);
  console.log(`scope: ${scope}`);
  console.log(`pubkey: ${pubkey}`);
  console.log("publish OK:");
  for (const result of publishResults) {
    console.log(`  kind ${result.kind}: ${result.id} (${result.ok})`);
  }
  console.log(`live subscription receipts: ${liveSeen.size}/${events.length}`);
  console.log(`stored query receipts excluding ephemeral 25910: ${stored.size}/${storedIds.length}`);
  console.log(`nip51 coordinate: ${coordinate}`);
  console.log(`nip51 coordinate present: ${queue.tags.some((tag) => tag[0] === "a" && tag[1] === coordinate)}`);
  console.log(`mutation method: ${mutationContent.method}`);
  console.log(`mutation target coordinate: ${mutationContent.params.coordinate}`);
  console.log(`mutation live receipt: ${liveSeen.has(mutation.id)}`);
  console.log(`rollback deletion event: ${deleteEvent.id} (${deleteResult})`);
}

main().catch((err) => {
  console.error(`fp-106 relay proof failed: ${err.message}`);
  process.exit(1);
});
