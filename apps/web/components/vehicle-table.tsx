"use client";

import {
  createColumnHelper,
  createPaginatedRowModel,
  createSortedRowModel,
  functionalUpdate,
  rowPaginationFeature,
  rowSortingFeature,
  sortFn_alphanumeric,
  sortFn_basic,
  tableFeatures,
  useTable,
  type SortDirection,
  type SortingState,
} from "@tanstack/react-table";
import { useState } from "react";
import { formatAgeSeconds, formatNullable, type VehicleRow } from "@/hooks/use-latest-vehicles";

const PAGE_SIZE = 50;

// Headless table: the library keeps the sorting and paging state, this file owns the markup.
// Features, columns and the initial sort are module-level so they keep their identity.
const features = tableFeatures({
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
  sortFns: { alphanumeric: sortFn_alphanumeric, basic: sortFn_basic },
  rowPaginationFeature,
  paginatedRowModel: createPaginatedRowModel(),
});

const MODE_LABEL: Record<string, string> = { bus: "Ônibus", brt: "BRT" };

const helper = createColumnHelper<typeof features, VehicleRow>();
const columns = helper.columns([
  // No line is undefined, not "": it sorts last in both directions and shows as a dash.
  helper.accessor((r) => r.line ?? undefined, {
    id: "line",
    header: "Linha",
    sortFn: "alphanumeric",
    sortUndefined: "last",
    cell: (c) => formatNullable(c.getValue() ?? null),
  }),
  helper.accessor("vehicle", { header: "Veículo", sortFn: "alphanumeric" }),
  helper.accessor((r) => MODE_LABEL[r.mode] ?? r.mode, { id: "mode", header: "Modo", sortFn: "alphanumeric" }),
  helper.accessor("operatorLabel", { header: "Operador", sortFn: "alphanumeric" }),
  helper.accessor((r) => r.ageSeconds ?? undefined, {
    id: "age",
    header: "Idade",
    sortFn: "basic",
    sortUndefined: "last",
    cell: (c) => formatAgeSeconds(c.getValue() ?? null),
  }),
  helper.accessor((r) => r.speed_kmh ?? undefined, {
    id: "speed",
    header: "Velocidade",
    sortFn: "basic",
    sortUndefined: "last",
    cell: (c) => formatNullable(c.getValue() ?? null, "km/h"),
  }),
  helper.accessor((r) => r.heading_deg ?? undefined, {
    id: "heading",
    header: "Direção",
    sortFn: "basic",
    sortUndefined: "last",
    cell: (c) => formatNullable(c.getValue() ?? null, "°"),
  }),
]);

const INITIAL_SORT: SortingState = [{ id: "line", desc: false }];

const ARIA_SORT = { asc: "ascending", desc: "descending" } as const;
const ARROW = { asc: "▲", desc: "▼" } as const;

type Props = {
  rows: VehicleRow[];
  // The page lives in the parent so that changing the filter can send the table back to page 1
  // in the same event handler.
  pageIndex: number;
  onPageIndexChange: (pageIndex: number) => void;
  caption: string;
};

export default function VehicleTable({ rows, pageIndex, onPageIndexChange, caption }: Props) {
  const [sorting, setSorting] = useState<SortingState>(INITIAL_SORT);
  const pageCount = Math.max(1, Math.ceil(rows.length / PAGE_SIZE));
  // A refresh or a filter can leave fewer pages than before.
  const page = Math.min(pageIndex, pageCount - 1);

  const table = useTable({
    features,
    columns,
    data: rows,
    state: { sorting, pagination: { pageIndex: page, pageSize: PAGE_SIZE } },
    onSortingChange: (updater) => {
      setSorting((prev) => functionalUpdate(updater, prev));
      onPageIndexChange(0);
    },
    onPaginationChange: (updater) => {
      onPageIndexChange(functionalUpdate(updater, { pageIndex: page, pageSize: PAGE_SIZE }).pageIndex);
    },
    // A new minute brings new data; the reader stays on the page they were reading.
    autoResetPageIndex: false,
    enableSortingRemoval: false,
    enableMultiSort: false,
  });

  return (
    <div>
      <div tabIndex={0} role="region" aria-label="Tabela de veículos, rolagem horizontal"
        className="overflow-x-auto">
        <table className="w-full border-collapse text-left text-xs">
          <caption className="pb-2 text-left text-sm text-calcadao">{caption}</caption>
          <thead>
            {table.getHeaderGroups().map((group) => (
              <tr key={group.id} className="border-b border-borda">
                {group.headers.map((header) => {
                  const sorted = header.column.getIsSorted() as SortDirection | false;
                  return (
                    <th
                      key={header.id}
                      scope="col"
                      aria-sort={sorted ? ARIA_SORT[sorted] : undefined}
                      className="py-1 pr-2 font-semibold"
                    >
                      <button
                        type="button"
                        onClick={header.column.getToggleSortingHandler()}
                        className="rounded px-1 py-0.5 font-semibold whitespace-nowrap hover:bg-accent"
                      >
                        {header.column.columnDef.header as string}
                        <span aria-hidden="true">{sorted ? ` ${ARROW[sorted]}` : ""}</span>
                      </button>
                    </th>
                  );
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {table.getRowModel().rows.map((row) => (
              <tr key={row.id} className="border-b border-border/40">
                {row.getAllCells().map((cell) => (
                  <td key={cell.id} className="py-1 pr-2 whitespace-nowrap">
                    <table.FlexRender cell={cell} />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <nav aria-label="Paginação da tabela" className="mt-2 flex items-center gap-3 text-sm">
        <button
          type="button"
          onClick={() => table.previousPage()}
          disabled={!table.getCanPreviousPage()}
          className="rounded border border-border px-2 py-1 enabled:hover:bg-accent disabled:text-neblina"
        >
          Anterior
        </button>
        <span>
          Página {page + 1} de {pageCount}
        </span>
        <button
          type="button"
          onClick={() => table.nextPage()}
          disabled={!table.getCanNextPage()}
          className="rounded border border-border px-2 py-1 enabled:hover:bg-accent disabled:text-neblina"
        >
          Próxima
        </button>
      </nav>
    </div>
  );
}
