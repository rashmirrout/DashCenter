import { useState, useMemo, useCallback } from 'react';
import { cn } from '@/lib/cn';

/* ── Types ─────────────────────────────────────────────────── */
export interface Column<T> {
  /** Unique key (used as React key and sort identifier) */
  key: string;
  /** Column header label */
  header: string;
  /** Extract cell value from row */
  accessor: (row: T) => unknown;
  /** Custom cell renderer */
  cell?: (row: T) => React.ReactNode;
  /** Enable sorting on this column (default: true) */
  sortable?: boolean;
  /** Column width class (e.g. 'w-32') */
  width?: string;
  /** Hide this column by default */
  hidden?: boolean;
  /** Alignment */
  align?: 'left' | 'center' | 'right';
}

export type SortDirection = 'asc' | 'desc';

export interface SortState {
  key: string;
  direction: SortDirection;
}

export interface DataTableProps<T> {
  /** Column definitions */
  columns: Column<T>[];
  /** Row data */
  data: T[];
  /** Unique key extractor for each row */
  rowKey: (row: T) => string;
  /** Click handler for row */
  onRowClick?: (row: T) => void;
  /** Initial sort state */
  defaultSort?: SortState;
  /** Number of rows per page (0 = no pagination) */
  pageSize?: number;
  /** Filter placeholder text */
  filterPlaceholder?: string;
  /** Enable text filter (default: true) */
  filterable?: boolean;
  /** Custom filter function */
  filterFn?: (row: T, query: string) => boolean;
  /** Empty state message */
  emptyMessage?: string;
  /** Additional class for the table container */
  className?: string;
  /** Whether to show the filter input */
  showFilter?: boolean;
  /** Compact row height */
  compact?: boolean;
  /** Sticky header */
  stickyHeader?: boolean;
}

/* ── Sort icon ─────────────────────────────────────────────── */
function SortIcon({ direction }: { direction?: SortDirection }) {
  return (
    <span className="inline-flex ml-1 text-text-muted" aria-hidden="true">
      {direction === 'asc' ? '▲' : direction === 'desc' ? '▼' : '⇅'}
    </span>
  );
}

/* ── DataTable ─────────────────────────────────────────────── */
export function DataTable<T>({
  columns,
  data,
  rowKey,
  onRowClick,
  defaultSort,
  pageSize = 20,
  filterPlaceholder = 'Filter…',
  filterable = true,
  filterFn,
  emptyMessage = 'No data available',
  className,
  showFilter = true,
  compact = false,
  stickyHeader = true,
}: DataTableProps<T>) {
  const [sort, setSort] = useState<SortState | null>(defaultSort ?? null);
  const [filter, setFilter] = useState('');
  const [page, setPage] = useState(0);

  // Visible columns
  const visibleColumns = useMemo(
    () => columns.filter((c) => !c.hidden),
    [columns],
  );

  // Default filter: stringify all accessor values and search
  const defaultFilterFn = useCallback(
    (row: T, query: string): boolean => {
      const q = query.toLowerCase();
      return visibleColumns.some((col) => {
        const val = col.accessor(row);
        return val != null && String(val).toLowerCase().includes(q);
      });
    },
    [visibleColumns],
  );

  const activeFilterFn = filterFn ?? defaultFilterFn;

  // Filtered data
  const filtered = useMemo(() => {
    if (!filterable || !filter) return data;
    return data.filter((row) => activeFilterFn(row, filter));
  }, [data, filter, filterable, activeFilterFn]);

  // Sorted data
  const sorted = useMemo(() => {
    if (!sort) return filtered;
    const col = columns.find((c) => c.key === sort.key);
    if (!col) return filtered;

    return [...filtered].sort((a, b) => {
      const aVal = col.accessor(a);
      const bVal = col.accessor(b);

      // Handle nulls
      if (aVal == null && bVal == null) return 0;
      if (aVal == null) return 1;
      if (bVal == null) return -1;

      // Numeric comparison
      if (typeof aVal === 'number' && typeof bVal === 'number') {
        return sort.direction === 'asc' ? aVal - bVal : bVal - aVal;
      }

      // String comparison
      const aStr = String(aVal);
      const bStr = String(bVal);
      const cmp = aStr.localeCompare(bStr);
      return sort.direction === 'asc' ? cmp : -cmp;
    });
  }, [filtered, sort, columns]);

  // Pagination
  const totalPages = pageSize > 0 ? Math.ceil(sorted.length / pageSize) : 1;
  const paginated = useMemo(() => {
    if (pageSize <= 0) return sorted;
    const start = page * pageSize;
    return sorted.slice(start, start + pageSize);
  }, [sorted, page, pageSize]);

  // Reset page when filter changes
  const handleFilterChange = useCallback((value: string) => {
    setFilter(value);
    setPage(0);
  }, []);

  // Toggle sort
  const handleSort = useCallback((key: string) => {
    setSort((prev) => {
      if (prev?.key === key) {
        if (prev.direction === 'asc') return { key, direction: 'desc' };
        if (prev.direction === 'desc') return null; // Clear sort
      }
      return { key, direction: 'asc' };
    });
  }, []);

  const cellAlign = (align?: string) =>
    align === 'right' ? 'text-right' : align === 'center' ? 'text-center' : 'text-left';

  return (
    <div className={cn('flex flex-col', className)}>
      {/* Toolbar */}
      {(showFilter && filterable) && (
        <div className="flex items-center justify-between mb-3">
          <input
            type="text"
            value={filter}
            onChange={(e) => handleFilterChange(e.target.value)}
            placeholder={filterPlaceholder}
            className="px-3 py-1.5 text-sm bg-bg-elevated border border-border rounded-lg text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 w-64"
            aria-label="Filter table"
          />
          <span className="text-xs text-text-muted">
            {filtered.length} of {data.length} rows
          </span>
        </div>
      )}

      {/* Table */}
      <div className="overflow-auto rounded-lg border border-border">
        <table className="w-full text-sm" role="grid">
          <thead className={cn(stickyHeader && 'sticky top-0 z-10')}>
            <tr className="bg-bg-elevated border-b border-border">
              {visibleColumns.map((col) => {
                const isSortable = col.sortable !== false;
                const isActive = sort?.key === col.key;
                return (
                  <th
                    key={col.key}
                    className={cn(
                      'px-3 font-medium text-text-secondary select-none',
                      compact ? 'py-1.5' : 'py-2',
                      col.width,
                      cellAlign(col.align),
                      isSortable && 'cursor-pointer hover:text-text-primary',
                    )}
                    onClick={isSortable ? () => handleSort(col.key) : undefined}
                    aria-sort={isActive ? (sort!.direction === 'asc' ? 'ascending' : 'descending') : undefined}
                    role="columnheader"
                  >
                    <span className="inline-flex items-center gap-0.5">
                      {col.header}
                      {isSortable && <SortIcon direction={isActive ? sort!.direction : undefined} />}
                    </span>
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody>
            {paginated.length === 0 ? (
              <tr>
                <td
                  colSpan={visibleColumns.length}
                  className="px-3 py-8 text-center text-text-muted"
                >
                  {emptyMessage}
                </td>
              </tr>
            ) : (
              paginated.map((row) => (
                <tr
                  key={rowKey(row)}
                  onClick={onRowClick ? () => onRowClick(row) : undefined}
                  className={cn(
                    'border-b border-border/50 transition-colors',
                    onRowClick && 'cursor-pointer hover:bg-bg-elevated/50',
                  )}
                  role="row"
                  tabIndex={onRowClick ? 0 : undefined}
                  onKeyDown={
                    onRowClick
                      ? (e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            onRowClick(row);
                          }
                        }
                      : undefined
                  }
                >
                  {visibleColumns.map((col) => (
                    <td
                      key={col.key}
                      className={cn(
                        'px-3',
                        compact ? 'py-1.5' : 'py-2',
                        col.width,
                        cellAlign(col.align),
                      )}
                    >
                      {col.cell ? col.cell(row) : String(col.accessor(row) ?? '—')}
                    </td>
                  ))}
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {pageSize > 0 && totalPages > 1 && (
        <div className="flex items-center justify-between mt-3">
          <span className="text-xs text-text-muted">
            Page {page + 1} of {totalPages}
          </span>
          <div className="flex gap-1">
            <button
              onClick={() => setPage(0)}
              disabled={page === 0}
              className="px-2 py-1 text-xs rounded border border-border hover:bg-bg-elevated disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
              aria-label="First page"
            >
              ⟨⟨
            </button>
            <button
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              disabled={page === 0}
              className="px-2 py-1 text-xs rounded border border-border hover:bg-bg-elevated disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
              aria-label="Previous page"
            >
              ⟨
            </button>
            <button
              onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
              disabled={page >= totalPages - 1}
              className="px-2 py-1 text-xs rounded border border-border hover:bg-bg-elevated disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
              aria-label="Next page"
            >
              ⟩
            </button>
            <button
              onClick={() => setPage(totalPages - 1)}
              disabled={page >= totalPages - 1}
              className="px-2 py-1 text-xs rounded border border-border hover:bg-bg-elevated disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
              aria-label="Last page"
            >
              ⟩⟩
            </button>
          </div>
        </div>
      )}
    </div>
  );
}