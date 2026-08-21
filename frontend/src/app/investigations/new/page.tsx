import { Suspense } from "react";

import { Loading } from "@/components/primitives";

import { StartInvestigationScreen } from "./StartInvestigationScreen";

export const metadata = { title: "Start investigation · Interbellum Console" };

export default function NewInvestigationPage() {
  return (
    <Suspense fallback={<Loading what="the ingestion form" />}>
      <StartInvestigationScreen />
    </Suspense>
  );
}
