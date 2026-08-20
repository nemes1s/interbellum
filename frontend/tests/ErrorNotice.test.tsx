import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ErrorNotice } from "@/components/ErrorNotice";
import { ApiError } from "@/lib/api-error";

/**
 * The backend promises a structured error envelope and expects clients to
 * branch on `code`. These assert that the console actually does — and that a
 * reviewer who trips one of the interesting 409s gets told what happened rather
 * than "Something went wrong".
 */
describe("ErrorNotice", () => {
  it("shows the code, the status and the server's own message", () => {
    render(
      <ErrorNotice
        error={
          new ApiError(409, {
            code: "INVALID_TRANSITION",
            message:
              "edge 5b1f7e2a originates at node a0000003, but the investigation is at node a0000001",
          })
        }
      />,
    );

    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.getByText("INVALID_TRANSITION")).toBeInTheDocument();
    expect(screen.getByText("HTTP 409")).toBeInTheDocument();
    expect(screen.getByText(/originates at node a0000003/)).toBeInTheDocument();
    expect(screen.getByText(/no longer available/i)).toBeInTheDocument();
    expect(screen.queryByText(/something went wrong/i)).not.toBeInTheDocument();
  });

  it.each([
    ["PLAYBOOK_VERSION_NOT_PUBLISHED", /still a draft/i],
    ["ALERT_TYPE_MISMATCH", /different alert type/i],
    ["INVESTIGATION_ALREADY_COMPLETED", /cannot be advanced/i],
    ["IDEMPOTENCY_KEY_REUSED", /idempotency key/i],
  ])("explains %s in words an analyst can act on", (code, expected) => {
    render(<ErrorNotice error={new ApiError(409, { code, message: "server detail" })} />);

    expect(screen.getByText(code)).toBeInTheDocument();
    expect(screen.getByText(expected)).toBeInTheDocument();
  });

  it("lists every graph validation issue returned with INVALID_PLAYBOOK_GRAPH", () => {
    render(
      <ErrorNotice
        error={
          new ApiError(422, {
            code: "INVALID_PLAYBOOK_GRAPH",
            message: "playbook graph is not valid for publishing (3 problem(s) found)",
            details: [
              { reason: "cycle_detected", node_id: "aa000000-0000-4000-8000-000000000001" },
              { reason: "cycle_detected", node_id: "aa000000-0000-4000-8000-000000000002" },
              { reason: "unreachable_from_root", node_id: "aa000000-0000-4000-8000-000000000003" },
            ],
          })
        }
      />,
    );

    expect(screen.getByText("3 graph problems")).toBeInTheDocument();
    expect(screen.getAllByText(/part of a cycle/i)).toHaveLength(2);
    expect(screen.getByText(/not reachable from the root node/i)).toBeInTheDocument();
    expect(screen.getByText(/node aa000000…0003/)).toBeInTheDocument();
  });

  it("still renders usefully for an error that is not an ApiError", () => {
    render(<ErrorNotice error={new TypeError("Failed to fetch")} />);

    expect(screen.getByText("UNEXPECTED_ERROR")).toBeInTheDocument();
    expect(screen.getByText("Failed to fetch")).toBeInTheDocument();
  });

  it("renders nothing when there is no error", () => {
    const { container } = render(<ErrorNotice error={null} />);

    expect(container).toBeEmptyDOMElement();
  });
});
