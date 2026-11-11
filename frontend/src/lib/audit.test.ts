import { describe, expect, it } from 'vitest';
import { auditSummary } from './audit';

describe('auditSummary', () => {
  it('names the task by key and title', () => {
    expect(auditSummary({ entity: 'task', data: { key: 'CREW-4', title: 'Radios', subtasks: 2 } })).toBe('CREW-4 Radios');
  });
  it('describes grants and marks the ones HR made', () => {
    expect(auditSummary({ entity: 'grant', data: { subject_type: 'group', subject: 'crew', level: 'view' } })).toBe(
      'group crew → view'
    );
    expect(
      auditSummary({
        entity: 'grant',
        data: { subject_type: 'group', subject: 'dept-bar', level: 'edit', source: 'hr_department_hook' },
      })
    ).toBe('group dept-bar → edit (HR)');
  });
  it('falls back to key and name for spaces and lists', () => {
    expect(auditSummary({ entity: 'space', data: { key: 'BAR', name: 'Bar' } })).toBe('BAR Bar');
    expect(auditSummary({ entity: 'list', data: { name: 'Backlog' } })).toBe('Backlog');
  });
});
