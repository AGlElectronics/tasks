import { describe, expect, it } from 'vitest';
import { describeRecurrence, formatMinutes, parseDuration } from './format';

describe('durations', () => {
  it('parses the forms people type', () => {
    expect(parseDuration('90')).toBe(90);
    expect(parseDuration('45m')).toBe(45);
    expect(parseDuration('1:30')).toBe(90);
    expect(parseDuration('1h30')).toBe(90);
    expect(parseDuration('1.5h')).toBe(90);
    expect(parseDuration('2 h')).toBe(120);
    expect(parseDuration('soon')).toBeNull();
  });
  it('formats minutes', () => {
    expect(formatMinutes(90)).toBe('1 h 30 min');
    expect(formatMinutes(120)).toBe('2 h');
    expect(formatMinutes(5)).toBe('5 min');
  });
  it('describes recurrence', () => {
    expect(describeRecurrence({ freq: 'weekly', interval: 1 })).toBe('Repeats every week');
    expect(describeRecurrence({ freq: 'monthly', interval: 3 })).toBe('Repeats every 3 months');
  });
});
