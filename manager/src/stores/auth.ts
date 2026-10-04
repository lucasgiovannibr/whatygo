import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';

interface AuthState {
  apiUrl: string;
  apiKey: string;
  isAuthenticated: boolean;
  setSession: (s: Partial<Pick<AuthState, 'apiUrl' | 'apiKey' | 'isAuthenticated'>>) => void;
  clear: () => void;
}

const defaultUrl = () => (typeof window !== 'undefined' ? window.location.origin : 'http://localhost:8080');

/**
 * The session lives in sessionStorage: it ends with the tab. It used to be kept in
 * localStorage, so the API key (the GLOBAL key, when an administrator logs in) stayed in the
 * browser for good and was readable by any script that ever ran on this origin. Whatever the
 * old versions left behind is removed.
 */
try {
  localStorage.removeItem('whatygo-auth');
} catch {
  /* storage blocked: nothing to clean */
}

export const useAuth = create<AuthState>()(
  persist(
    (set) => ({
      apiUrl: defaultUrl(),
      apiKey: '',
      isAuthenticated: false,
      setSession: (s) => set(s),
      clear: () => set({ apiUrl: defaultUrl(), apiKey: '', isAuthenticated: false }),
    }),
    {
      name: 'whatygo-auth',
      storage: createJSONStorage(() => sessionStorage),
      partialize: (s) => ({
        apiUrl: s.apiUrl,
        apiKey: s.apiKey,
        isAuthenticated: s.isAuthenticated,
      }),
    },
  ),
);

export const isSignedIn = (s: Pick<AuthState, 'isAuthenticated'>) => s.isAuthenticated;
