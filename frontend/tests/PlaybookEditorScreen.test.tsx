import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { PlaybookEditorScreen } from "@/app/playbooks/PlaybookEditorScreen";
import type { PlaybookVersionDefinition, PlaybookWithVersions } from "@/lib/types";

// The preview is React Flow, which needs a real layout box and a ResizeObserver
// neither of which jsdom provides. These tests are about the editor's own
// behaviour, so it is stubbed rather than given an environment to run in.
vi.mock("@/components/graph/PlaybookGraph", () => ({
  PlaybookGraph: () => <div data-testid="preview" />,
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}));

vi.mock("@/lib/api", () => ({
  getPlaybook: vi.fn(),
  getPlaybookVersion: vi.fn(),
  createPlaybook: vi.fn(),
  replacePlaybookVersionGraph: vi.fn(),
  publishPlaybookVersion: vi.fn(),
  createPlaybookVersion: vi.fn(),
}));

const api = await import("@/lib/api");

const PLAYBOOK_A = "a0000000-0000-4000-8000-000000000001";
const PLAYBOOK_B = "b0000000-0000-4000-8000-000000000002";
const VERSION_OF_B = "c0000000-0000-4000-8000-000000000003";

function playbook(id: string, name: string): PlaybookWithVersions {
  return {
    id,
    name,
    description: "",
    alert_type: "unauthorized_plc_register_write",
    created_at: "2026-08-19T10:00:00Z",
    updated_at: "2026-08-19T10:00:00Z",
    versions: [],
  };
}

function draftVersion(id: string, playbookId: string): PlaybookVersionDefinition {
  return {
    id,
    playbook_id: playbookId,
    version: 1,
    status: "draft",
    created_at: "2026-08-19T10:00:00Z",
    published_at: null,
    root_node_id: null,
    nodes: [],
    edges: [],
  };
}

beforeEach(() => {
  vi.mocked(api.getPlaybook).mockReset();
  vi.mocked(api.getPlaybookVersion).mockReset();
  vi.mocked(api.createPlaybook).mockReset();
  vi.mocked(api.replacePlaybookVersionGraph).mockReset();
});

/**
 * Both ids in `/playbooks/{playbookId}/versions/{versionId}/edit` are resolved
 * independently, so the pairing itself is only ever as true as the URL. These
 * cover what happens when it is not.
 */
describe("PlaybookEditorScreen route guard", () => {
  it("refuses to edit a version that belongs to a different playbook", async () => {
    vi.mocked(api.getPlaybook).mockResolvedValue(playbook(PLAYBOOK_A, "Playbook A"));
    vi.mocked(api.getPlaybookVersion).mockResolvedValue(draftVersion(VERSION_OF_B, PLAYBOOK_B));

    render(<PlaybookEditorScreen playbookId={PLAYBOOK_A} versionId={VERSION_OF_B} />);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /belongs to playbook .* not to the one named in this URL/,
    );
    // Not merely a warning above an editable form: there is no form at all.
    expect(screen.queryByRole("button", { name: /Save draft/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Publish version/ })).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Root node")).not.toBeInTheDocument();
  });

  it("offers the version's real address rather than silently redirecting to it", async () => {
    vi.mocked(api.getPlaybook).mockResolvedValue(playbook(PLAYBOOK_A, "Playbook A"));
    vi.mocked(api.getPlaybookVersion).mockResolvedValue(draftVersion(VERSION_OF_B, PLAYBOOK_B));

    render(<PlaybookEditorScreen playbookId={PLAYBOOK_A} versionId={VERSION_OF_B} />);

    const link = await screen.findByRole("link", { name: "Open it under its own playbook" });
    expect(link).toHaveAttribute("href", `/playbooks/${PLAYBOOK_B}/versions/${VERSION_OF_B}/edit`);
  });

  it("edits normally when the version does belong to the playbook in the path", async () => {
    vi.mocked(api.getPlaybook).mockResolvedValue(playbook(PLAYBOOK_A, "Playbook A"));
    vi.mocked(api.getPlaybookVersion).mockResolvedValue(draftVersion(VERSION_OF_B, PLAYBOOK_A));

    render(<PlaybookEditorScreen playbookId={PLAYBOOK_A} versionId={VERSION_OF_B} />);

    expect(await screen.findByRole("button", { name: "Publish version" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Open it under its own playbook" })).not.toBeInTheDocument();
  });

  it("surfaces a failure to load the playbook, not only a failure to load the version", async () => {
    vi.mocked(api.getPlaybook).mockRejectedValue(
      new (await import("@/lib/api-error")).ApiError(404, {
        code: "RESOURCE_NOT_FOUND",
        message: "playbook not found",
      }),
    );
    vi.mocked(api.getPlaybookVersion).mockResolvedValue(draftVersion(VERSION_OF_B, PLAYBOOK_A));

    render(<PlaybookEditorScreen playbookId={PLAYBOOK_A} versionId={VERSION_OF_B} />);

    expect(await screen.findByText("RESOURCE_NOT_FOUND")).toBeInTheDocument();
    expect(screen.getByText("playbook not found")).toBeInTheDocument();
  });
});

/**
 * A client-side error describes the draft at the moment Save was pressed. Once
 * the designer starts fixing it, it is describing something that no longer
 * exists — and for draft errors, whose `index` is positional, it can point at
 * the wrong row entirely.
 */
describe("PlaybookEditorScreen stale validation errors", () => {
  it("drops the name error as soon as the name is typed into", async () => {
    const user = userEvent.setup();
    render(<PlaybookEditorScreen />);

    await user.type(screen.getByLabelText("Alert type"), "suspicious_outbound_ot_connection");
    await user.click(screen.getByRole("button", { name: "Create draft" }));

    expect(await screen.findByText("A name is required.")).toBeInTheDocument();
    expect(api.createPlaybook).not.toHaveBeenCalled();

    await user.type(screen.getByLabelText("Name"), "S");

    await waitFor(() => expect(screen.queryByText("A name is required.")).not.toBeInTheDocument());
  });

  it("drops the alert type error independently of the name error", async () => {
    const user = userEvent.setup();
    render(<PlaybookEditorScreen />);

    await user.click(screen.getByRole("button", { name: "Create draft" }));

    expect(await screen.findByText("A name is required.")).toBeInTheDocument();
    expect(screen.getByText("An alert type is required.")).toBeInTheDocument();

    await user.type(screen.getByLabelText("Alert type"), "x");

    await waitFor(() =>
      expect(screen.queryByText("An alert type is required.")).not.toBeInTheDocument(),
    );
    // The name is still blank, so its error is still true.
    expect(screen.getByText("A name is required.")).toBeInTheDocument();
  });

  it("drops a draft graph error as soon as the draft changes", async () => {
    const user = userEvent.setup();
    render(<PlaybookEditorScreen />);

    await user.type(screen.getByLabelText("Name"), "Playbook");
    await user.type(screen.getByLabelText("Alert type"), "some_alert");
    await user.click(screen.getByRole("button", { name: "+ Decision node" }));
    await user.click(screen.getByRole("button", { name: "Create draft" }));

    expect(await screen.findByText("Title is required.")).toBeInTheDocument();
    expect(api.createPlaybook).not.toHaveBeenCalled();

    await user.type(screen.getByLabelText("Title"), "Known workstation?");

    await waitFor(() => expect(screen.queryByText("Title is required.")).not.toBeInTheDocument());
    // The summary counting the rejected fields goes with them.
    expect(screen.queryByText(/would be rejected by the draft write/)).not.toBeInTheDocument();
  });
});
