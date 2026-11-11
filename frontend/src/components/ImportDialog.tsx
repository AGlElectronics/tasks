import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { X } from 'lucide-react';
import { ApiError, apiRequest } from '@/api/client';
import { Button } from '@/components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Label } from '@/components/ui/Input';

type ImportFormat = 'csv' | 'clickup' | 'vikunja';
type ImportResult = {
  dry_run: boolean;
  created: number;
  skipped: number;
  new_tags: string[];
  warnings: string[];
  preview: { line: number; title: string; status: string; due_at: string | null; assignees: string[]; tags: string[] }[];
};

const importTasks = (listId: string, body: { format: ImportFormat; content: string; dry_run: boolean }) =>
  apiRequest<ImportResult>(`/api/v1/lists/${listId}/import`, { method: 'POST', body });

const HELP: Record<ImportFormat, string> = {
  csv: 'A header row with Title (required) and any of Description, Status, Priority, Due Date, Start Date, Assignees (emails), Tags.',
  clickup: 'ClickUp → Export → CSV, as it comes.',
  vikunja: 'The data.json from a Vikunja export (Settings → Export your data), unzipped.',
};

/** Import tasks into a list: pick a file, check the preview, then import. */
export function ImportDialog({ listId, onClose }: { listId: string; onClose: () => void }) {
  const qc = useQueryClient();
  const [format, setFormat] = useState<ImportFormat>('csv');
  const [content, setContent] = useState('');
  const [fileName, setFileName] = useState('');
  const preview = useMutation({ mutationFn: () => importTasks(listId, { format, content, dry_run: true }) });
  const run = useMutation({
    mutationFn: () => importTasks(listId, { format, content, dry_run: false }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['tasks', listId] });
      void qc.invalidateQueries({ queryKey: ['spaces'] });
      void qc.invalidateQueries({ queryKey: ['tags'] });
    },
  });
  const result = run.data ?? preview.data;
  const error = run.error ?? preview.error;
  return (
    <div className="fixed inset-0 z-50 bg-black/40 flex items-start justify-center p-4 overflow-y-auto" onClick={onClose}>
      <Card className="w-full max-w-2xl mt-12" onClick={(e) => e.stopPropagation()}>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle>Import tasks</CardTitle>
          <button type="button" onClick={onClose} aria-label="Close" className="text-muted-foreground hover:text-foreground">
            <X className="w-4 h-4" />
          </button>
        </CardHeader>
        <CardContent className="space-y-4 text-sm">
          <div className="grid gap-3 sm:grid-cols-[10rem_1fr]">
            <div>
              <Label htmlFor="imp-format">From</Label>
              <select
                id="imp-format"
                value={format}
                onChange={(e) => {
                  setFormat(e.target.value as ImportFormat);
                  preview.reset();
                  run.reset();
                }}
                className="w-full h-9 rounded-md border border-input bg-background px-2"
              >
                <option value="csv">CSV</option>
                <option value="clickup">ClickUp (CSV)</option>
                <option value="vikunja">Vikunja (JSON)</option>
              </select>
            </div>
            <div>
              <Label htmlFor="imp-file">File (up to 5 MB, 2,000 tasks)</Label>
              <input
                id="imp-file"
                type="file"
                accept={format === 'vikunja' ? '.json,application/json' : '.csv,text/csv'}
                onChange={async (e) => {
                  const f = e.target.files?.[0];
                  if (!f) return;
                  setFileName(f.name);
                  setContent(await f.text());
                  preview.reset();
                  run.reset();
                }}
                className="block w-full text-sm"
              />
            </div>
          </div>
          <p className="text-xs text-muted-foreground">
            {HELP[format]} Statuses match this space’s by name; people match by email or name if they have signed in to
            Tasks; unknown tags are created if you manage the space.
          </p>
          <div className="flex gap-2">
            <Button size="sm" variant="outline" disabled={!content || preview.isPending} onClick={() => preview.mutate()}>
              Check {fileName || 'file'}
            </Button>
            <Button
              size="sm"
              disabled={!preview.data || preview.data.created === 0 || run.isPending || run.isSuccess}
              onClick={() => run.mutate()}
            >
              Import {preview.data ? preview.data.created : ''} tasks
            </Button>
          </div>
          {error ? <p className="text-destructive">{error instanceof ApiError ? error.message : 'The import failed.'}</p> : null}
          {result ? (
            <div className="space-y-2">
              <p className={run.isSuccess ? 'font-semibold text-emerald-600' : 'font-semibold'}>
                {run.isSuccess ? `Imported ${result.created} tasks.` : `${result.created} tasks will be created`}
                {result.skipped ? `, ${result.skipped} rows skipped` : ''}
                {result.new_tags.length ? `, new tags: ${result.new_tags.join(', ')}` : ''}.
              </p>
              {result.warnings.length ? (
                <ul className="text-xs text-amber-700 dark:text-amber-300 list-disc pl-5 max-h-40 overflow-y-auto">
                  {result.warnings.map((w, i) => (
                    <li key={i}>{w}</li>
                  ))}
                </ul>
              ) : null}
              {!run.isSuccess && result.preview.length ? (
                <table className="w-full text-xs">
                  <thead className="text-muted-foreground text-left">
                    <tr>
                      <th className="py-1">Line</th>
                      <th>Title</th>
                      <th>Status</th>
                      <th>Due</th>
                      <th>Assignees</th>
                    </tr>
                  </thead>
                  <tbody>
                    {result.preview.map((r) => (
                      <tr key={r.line} className="border-t border-border">
                        <td className="py-1">{r.line}</td>
                        <td className="truncate max-w-[14rem]">{r.title}</td>
                        <td>{r.status}</td>
                        <td>{r.due_at ?? ''}</td>
                        <td>{r.assignees.join(', ')}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : null}
            </div>
          ) : null}
        </CardContent>
      </Card>
    </div>
  );
}
