import { lazy, Suspense } from 'react';
import { BrowserRouter, Navigate, Outlet, Route, Routes, useParams } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from 'sonner';
import { ApiError } from '@/lib/http';
import { isSignedIn, useAuth } from '@/stores/auth';
import { useUi } from '@/stores/ui';
import { AppShell } from '@/components/layout/app-shell';
import { Spinner } from '@/components/ui/feedback';
import { LoginPage } from '@/features/auth/login-page';
import { InstanceActionsProvider } from '@/features/instances/actions';
import { InstancesPage } from '@/features/instances/instances-page';
import { InstancePage } from '@/features/instance-detail/instance-page';
import { TabBehavior } from '@/features/instance-detail/tab-behavior';
import { TabCalls } from '@/features/instance-detail/tab-calls';
import { TabGeneral } from '@/features/instance-detail/tab-general';
import { TabTest } from '@/features/instance-detail/tab-test';
import { TabWebhook } from '@/features/instance-detail/tab-webhook';
import { NotFoundPage } from '@/features/not-found';
import { OverviewPage } from '@/features/overview/overview-page';

// The explorer pulls in the swagger tooling; keep it out of the first paint.
const ExplorerPage = lazy(() => import('@/features/explorer/explorer-page').then((m) => ({ default: m.ExplorerPage })));

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      refetchOnWindowFocus: true,
      // Client errors (bad key, missing record) will not fix themselves on retry.
      retry: (count, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && count < 2,
    },
  },
});

function RequireAuth() {
  const signedIn = useAuth(isSignedIn);
  if (!signedIn) return <Navigate to="/manager/login" replace />;
  return (
    <InstanceActionsProvider>
      <Outlet />
    </InstanceActionsProvider>
  );
}

/** The old manager exposed /instances/:id/settings; keep those bookmarks working. */
function LegacySettingsRedirect() {
  const { instanceId } = useParams();
  return <Navigate to={`/manager/instances/${instanceId}/webhook`} replace />;
}

function ThemedToaster() {
  const theme = useUi((s) => s.theme);
  return (
    <Toaster
      theme={theme}
      position="top-center"
      closeButton
      style={
        {
          '--normal-bg': 'var(--surface)',
          '--normal-text': 'var(--fg)',
          '--normal-border': 'var(--line-strong)',
          '--border-radius': '0.75rem',
          fontFamily: 'var(--font-sans)',
        } as React.CSSProperties
      }
    />
  );
}

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Navigate to="/manager" replace />} />
          <Route path="/manager/login" element={<LoginPage />} />

          <Route path="/manager" element={<RequireAuth />}>
            <Route element={<AppShell />}>
              <Route index element={<OverviewPage />} />
              <Route path="instances" element={<InstancesPage />} />
              <Route path="instances/:instanceId" element={<InstancePage />}>
                <Route index element={<TabGeneral />} />
                <Route path="webhook" element={<TabWebhook />} />
                <Route path="behavior" element={<TabBehavior />} />
                <Route path="calls" element={<TabCalls />} />
                <Route path="test" element={<TabTest />} />
              </Route>
              <Route path="instances/:instanceId/settings" element={<LegacySettingsRedirect />} />
              <Route
                path="api-tester"
                element={
                  <Suspense fallback={<div className="flex h-[60dvh] items-center justify-center"><Spinner className="size-6" /></div>}>
                    <ExplorerPage />
                  </Suspense>
                }
              />
              <Route path="messages" element={<Navigate to="/manager/instances" replace />} />
              <Route path="events" element={<Navigate to="/manager/instances" replace />} />
              <Route path="settings" element={<Navigate to="/manager" replace />} />
              <Route path="*" element={<NotFoundPage />} />
            </Route>
          </Route>

          <Route path="*" element={<Navigate to="/manager" replace />} />
        </Routes>
      </BrowserRouter>
      <ThemedToaster />
    </QueryClientProvider>
  );
}
