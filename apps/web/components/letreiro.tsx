"use client";

import { useState } from "react";
import { useReducedMotion } from "@/hooks/use-reduced-motion";

// The signature: the amber dot-matrix destination sign on the front of every Rio bus. Dark in both
// colour schemes, as real signs are. Doto is used only here, and only at 20 px or larger: dot-matrix
// text can't be read any smaller.
//
// The text is real text, so screen readers read it. When it changes, the old text rolls up and out and
// the new text rolls in from below in 300 ms, like a real sign scrolling. The outgoing copy is
// aria-hidden; with prefers-reduced-motion there is no copy and no motion.
export default function Letreiro({ text }: { text: string }) {
  const reduced = useReducedMotion();
  const [shown, setShown] = useState(text);
  const [leaving, setLeaving] = useState<string | null>(null);

  // Derived while rendering, not in an Effect: a new text starts the roll in the same render.
  if (text !== shown) {
    setShown(text);
    setLeaving(reduced ? null : shown);
  }

  return (
    <h1
      tabIndex={0}
      className="letreiro-grid pointer-events-auto rounded-lg border border-borda bg-letreiro-bg px-4 py-2.5 font-letreiro text-[22px] leading-tight font-bold tracking-wide text-letreiro uppercase sm:text-[26px]"
    >
      <span className="grid overflow-hidden">
        {leaving !== null && (
          <span
            aria-hidden="true"
            className="col-start-1 row-start-1 animate-roll-out motion-reduce:animate-none"
            onAnimationEnd={() => setLeaving(null)}
          >
            {leaving}
          </span>
        )}
        <span key={shown} className={`col-start-1 row-start-1 ${leaving !== null ? "animate-roll-in motion-reduce:animate-none" : ""}`}>
          {shown}
        </span>
      </span>
    </h1>
  );
}

// A small, still sign for a line number (the bus card). Same face, same 20 px floor.
export function LineSign({ children }: { children: React.ReactNode }) {
  return (
    <p className="letreiro-grid inline-block rounded-md border border-borda bg-letreiro-bg px-3 py-1 font-letreiro text-[22px] leading-tight font-bold tracking-wide text-letreiro uppercase">
      {children}
    </p>
  );
}
