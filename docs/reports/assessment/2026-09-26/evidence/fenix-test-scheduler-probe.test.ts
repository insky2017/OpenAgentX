// Assessment-only behavior probe: ORIGINAL scheduler executor, synthetic PromptTurn.
// These tests document observed behavior; pass does NOT mean correct completion semantics.
import { afterEach, expect, test } from "bun:test";
import { agentExecutor, setAgentExecutorDeps } from "/tmp/oax-fenix-assessment-20260926/src/services/scheduler/agent-executor.ts";

afterEach(() => setAgentExecutorDeps(null));
const input = (timeoutSeconds = 1) => ({
  triggeredBy: "manual",
  task: { id: "assessment-synthetic", userId: "synthetic-user", organizationId: "synthetic-org", agentId: "synthetic-agent", definition: { prompt: "synthetic" }, timeoutSeconds },
}) as never;
function setEvents(events: unknown[], openDelayMs = 0) {
  let disposed = 0;
  setAgentExecutorDeps({openAgentSession: async () => {
    if (openDelayMs) await new Promise(resolve => setTimeout(resolve, openDelayMs));
    return { instanceId: "synthetic-instance", turn: {
      prompt() {},
      async *events() { for (const event of events) yield event; },
      async dispose() { disposed++; },
    } } as never;
  }});
  return () => disposed;
}
for (const reason of ["end_turn", "cancelled", "max_tokens", "error"]) {
  test(`observed scheduler status for stopReason=${reason}`, async () => {
    const disposed = setEvents([{jsonrpc: "2.0", result: {stopReason: reason}}]);
    const output = await agentExecutor.execute(input());
    console.log(JSON.stringify({probe: "stopReason", reason, output, disposed: disposed()}));
    expect(output.status).toBe("success");
    expect(disposed()).toBe(1);
  });
}
test("observed empty event stream is success without a terminal record", async () => {
  setEvents([]);
  const output = await agentExecutor.execute(input());
  console.log(JSON.stringify({probe: "empty-stream", output}));
  expect(output.status).toBe("success");
  expect(output.resultSummary).toBe("");
});
test("observed openAgentSession delay is outside timeout budget", async () => {
  setEvents([{jsonrpc: "2.0", result: {stopReason: "end_turn"}}], 40);
  const output = await agentExecutor.execute(input(0.005));
  console.log(JSON.stringify({probe: "open-delay", configuredTimeoutMs: 5, injectedOpenDelayMs: 40, output}));
  expect(output.status).toBe("success");
  expect(output.duration).toBeGreaterThanOrEqual(35);
});
