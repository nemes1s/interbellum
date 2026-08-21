import { PlaybookEditorScreen } from "@/app/playbooks/PlaybookEditorScreen";

export const metadata = { title: "Edit playbook version · Interbellum Console" };

/**
 * Both ids live in the path rather than in component state, so a draft being
 * authored is refreshable, bookmarkable and shareable like every other screen.
 */
export default async function EditPlaybookVersionPage({
  params,
}: {
  params: Promise<{ playbookId: string; versionId: string }>;
}) {
  const { playbookId, versionId } = await params;
  return <PlaybookEditorScreen playbookId={playbookId} versionId={versionId} />;
}
