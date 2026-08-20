import { InvestigationRunner } from "./InvestigationRunner";

export const metadata = { title: "Investigation · Interbellum Console" };

export default async function InvestigationPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <InvestigationRunner investigationId={id} />;
}
