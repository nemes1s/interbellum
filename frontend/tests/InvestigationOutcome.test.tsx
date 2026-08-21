import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { InvestigationOutcome } from "@/components/InvestigationOutcome";
import type { PlaybookNode } from "@/lib/types";

const terminalNode: PlaybookNode = {
  id: "b0000000-0000-4000-8000-000000000001",
  kind: "terminal",
  title: "Close: authorized maintenance",
  description: "Known workstation, approved window, non-safety register.",
  terminal_resolution: "close_authorized_maintenance",
  metadata: null,
};

/**
 * What a reviewer sees when an investigation finishes: the resolution token
 * verbatim, the outcome node, and the way through to the audit report.
 */
describe("InvestigationOutcome", () => {
  function renderCompleted(overrides: Partial<React.ComponentProps<typeof InvestigationOutcome>> = {}) {
    return render(
      <InvestigationOutcome
        investigationId="3f2a9d10-0000-4000-8000-00000000abcd"
        finalResolution="close_authorized_maintenance"
        terminalNode={terminalNode}
        startedAt="2026-08-19T10:30:00Z"
        completedAt="2026-08-19T10:34:12Z"
        {...overrides}
      />,
    );
  }

  it("shows the resolution token exactly as the playbook author wrote it", () => {
    renderCompleted();

    // The raw token is what downstream systems key on, so it is never paraphrased away.
    expect(screen.getByText("close_authorized_maintenance")).toBeInTheDocument();
    expect(screen.getByText("Close authorized maintenance")).toBeInTheDocument();
  });

  it("marks the investigation completed and names the terminal node it reached", () => {
    renderCompleted();

    expect(screen.getByText("completed")).toBeInTheDocument();
    expect(screen.getByText("Terminal resolution reached")).toBeInTheDocument();
    expect(screen.getByText("Close: authorized maintenance")).toBeInTheDocument();
    expect(screen.getByText(/non-safety register/)).toBeInTheDocument();
  });

  it("offers the report, linked to this investigation", () => {
    renderCompleted();

    const link = screen.getByRole("link", { name: "View investigation report" });
    expect(link).toHaveAttribute("href", "/investigations/3f2a9d10-0000-4000-8000-00000000abcd/report");
  });

  it("reports completion time in absolute UTC and shows how long the run took", () => {
    renderCompleted();

    expect(screen.getByText(/19 Aug 2026 10:34:12 UTC/)).toBeInTheDocument();
    expect(screen.getByText("4m 12s elapsed")).toBeInTheDocument();
  });

  it("can be rendered without the report link, for the report page itself", () => {
    renderCompleted({ showReportLink: false });

    expect(screen.queryByRole("link", { name: "View investigation report" })).not.toBeInTheDocument();
  });
});
