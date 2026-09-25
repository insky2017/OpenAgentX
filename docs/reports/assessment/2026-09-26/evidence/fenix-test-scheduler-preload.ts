// Assessment-only dependency isolation. Not an upstream fixture.
// Prevent importing host DB/auth/runtime/bootstrap. Scheduler implementation remains original.
import { mock } from "bun:test";
mock.module("/tmp/oax-fenix-assessment-20260926/src/services/agent-chat-service.ts", () => ({
  openAgentSession: async () => { throw new Error("Assessment probe must inject synthetic turn"); },
}));
mock.module("@fenix/logger", () => ({ log: () => {}, error: () => {} }));
