import { InvestigationReportScreen } from "./InvestigationReportScreen";

export const metadata = { title: "Investigation report · Interbellum Console" };

export default async function ReportPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <InvestigationReportScreen investigationId={id} />;
}
