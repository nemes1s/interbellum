import { Suspense } from "react";

import { Loading } from "@/components/primitives";

import { PlaybooksScreen } from "./PlaybooksScreen";

export const metadata = { title: "Playbooks · Interbellum Console" };

export default function PlaybooksPage() {
  return (
    <Suspense fallback={<Loading what="playbooks" />}>
      <PlaybooksScreen />
    </Suspense>
  );
}
