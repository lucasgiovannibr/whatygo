import { describe, expect, it } from 'vitest';
import { isSignedIn, useAuth } from './auth';

describe('session', () => {
  it('depends only on the key having been accepted, with no license step', () => {
    expect(isSignedIn({ isAuthenticated: true })).toBe(true);
    expect(isSignedIn({ isAuthenticated: false })).toBe(false);
  });

  it('clear() ends the session', () => {
    useAuth.getState().setSession({ apiKey: 'k', isAuthenticated: true });
    useAuth.getState().clear();
    expect(isSignedIn(useAuth.getState())).toBe(false);
    expect(useAuth.getState().apiKey).toBe('');
  });
});
