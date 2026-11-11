import { describe, expect, it } from 'vitest';
import type { CustomField } from '@/api/tasks';
import { formatFieldValue, parseFieldInput } from './fields';

const field = (type: CustomField['type'], options: CustomField['options'] = []): CustomField => ({
  id: 'f',
  space_id: 's',
  name: 'F',
  type,
  options,
  position: 0,
});

describe('custom field values', () => {
  it('formats select options by name and checkboxes as Yes/No', () => {
    expect(formatFieldValue(field('select', [{ id: 'o1', name: 'Ordered', color: '#000000' }]), 'o1')).toBe('Ordered');
    expect(formatFieldValue(field('checkbox'), true)).toBe('Yes');
    expect(formatFieldValue(field('text'), undefined)).toBe('');
  });
  it('parses numbers (comma or dot) and clears empty input', () => {
    expect(parseFieldInput(field('number'), '12,5')).toBe(12.5);
    expect(parseFieldInput(field('number'), 'abc')).toBeNull();
    expect(parseFieldInput(field('text'), '  ')).toBeNull();
    expect(parseFieldInput(field('checkbox'), false)).toBe(false);
  });
});
