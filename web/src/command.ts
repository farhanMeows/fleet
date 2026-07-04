export type ParsedCommand =
  | { kind: "dispatch"; project: string; prompt: string; force: boolean }
  | { kind: "help" }
  | { kind: "error"; message: string }
  | { kind: "empty" };

// Parses a command-bar line. Supported:
//   dispatch <project>: <prompt>
//   d <project>: <prompt>
//   force-dispatch <project>: <prompt>
//   help
export function parseCommand(raw: string): ParsedCommand {
  const line = raw.trim();
  if (!line) return { kind: "empty" };

  const lower = line.toLowerCase();
  if (lower === "help" || lower === "?") return { kind: "help" };

  const m = /^(\S+)\s+([\s\S]+)$/.exec(line);
  if (!m) {
    return { kind: "error", message: `unknown command: ${line}` };
  }
  const verb = m[1].toLowerCase();
  const rest = m[2];

  let force = false;
  if (verb === "dispatch" || verb === "d") {
    force = false;
  } else if (verb === "force-dispatch" || verb === "fd") {
    force = true;
  } else {
    return { kind: "error", message: `unknown command: ${verb}` };
  }

  const colon = rest.indexOf(":");
  if (colon === -1) {
    return {
      kind: "error",
      message: `usage: ${verb} <project>: <prompt>`,
    };
  }
  const project = rest.slice(0, colon).trim();
  const prompt = rest.slice(colon + 1).trim();
  if (!project) return { kind: "error", message: "missing project name" };
  if (!prompt) return { kind: "error", message: "missing prompt" };

  return { kind: "dispatch", project, prompt, force };
}
