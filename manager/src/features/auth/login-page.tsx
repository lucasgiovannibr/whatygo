import { useState, type FormEvent } from 'react';
import { Navigate, useNavigate } from 'react-router-dom';
import { ArrowRight, Eye, EyeOff, KeyRound, Server } from 'lucide-react';
import { EXPIRED_FLAG } from '@/lib/http';
import { hostOf, isHttpUrl } from '@/lib/format';
import { verifyApiKey } from '@/api/session';
import { isSignedIn, useAuth } from '@/stores/auth';
import { useDocumentTitle } from '@/hooks/use-document-title';
import { Button } from '@/components/ui/button';
import { Field, Input } from '@/components/ui/form';
import { Alert } from '@/components/ui/feedback';
import { AuthLayout } from './auth-layout';

export function LoginPage() {
  useDocumentTitle('Entrar');
  const navigate = useNavigate();
  const signedIn = useAuth(isSignedIn);
  const savedUrl = useAuth((s) => s.apiUrl);
  const savedKey = useAuth((s) => s.apiKey);
  const setSession = useAuth((s) => s.setSession);

  const [url, setUrl] = useState(savedUrl || window.location.origin);
  const [key, setKey] = useState(savedKey);
  const [showKey, setShowKey] = useState(false);
  const [editServer, setEditServer] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice] = useState(() => {
    try {
      const v = sessionStorage.getItem(EXPIRED_FLAG);
      sessionStorage.removeItem(EXPIRED_FLAG);
      return v ? 'Sua sessão expirou ou a chave não é mais válida. Entre novamente.' : '';
    } catch {
      return '';
    }
  });

  if (signedIn) return <Navigate to="/manager" replace />;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError('');
    const base = url.trim().replace(/\/+$/, '');
    const apikey = key.trim();
    if (!isHttpUrl(base)) {
      setEditServer(true);
      return setError('A URL do servidor deve começar com http:// ou https://');
    }
    if (!apikey) return setError('Informe a API Key.');

    try {
      setBusy(true);
      await verifyApiKey(base, apikey);
      setSession({ apiUrl: base, apiKey: apikey, isAuthenticated: true });
      navigate('/manager', { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Erro ao conectar.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthLayout>
      <div className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight">Entrar</h1>
        <p className="mt-1 text-[13px] text-muted">Use a chave global (<code className="font-mono text-xs">GLOBAL_API_KEY</code>) do servidor.</p>
      </div>

      <form onSubmit={submit} className="space-y-4" noValidate>
        {error ? <Alert tone="danger">{error}</Alert> : notice ? <Alert tone="warn">{notice}</Alert> : null}

        <Field label="API Key">
          {(id) => (
            <Input
              id={id}
              data-autofocus
              autoFocus
              type={showKey ? 'text' : 'password'}
              value={key}
              onChange={(e) => setKey(e.target.value)}
              placeholder="Sua chave de API"
              autoComplete="current-password"
              spellCheck={false}
              leading={<KeyRound />}
              trailing={
                <Button variant="ghost" size="icon-sm" onClick={() => setShowKey((v) => !v)} aria-label={showKey ? 'Ocultar chave' : 'Mostrar chave'}>
                  {showKey ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                </Button>
              }
            />
          )}
        </Field>

        {editServer ? (
          <Field label="URL do servidor" hint="Padrão: o mesmo endereço em que este painel está aberto.">
            {(id) => <Input id={id} value={url} onChange={(e) => setUrl(e.target.value)} placeholder={window.location.origin} leading={<Server />} spellCheck={false} inputMode="url" />}
          </Field>
        ) : (
          <p className="flex items-center gap-2 text-xs text-muted">
            <Server className="size-3.5 text-subtle" />
            Servidor: <span className="font-mono text-fg">{hostOf(url)}</span>
            <button type="button" onClick={() => setEditServer(true)} className="ml-auto rounded font-medium text-brand-text hover:underline">
              Alterar
            </button>
          </p>
        )}

        <Button type="submit" variant="primary" size="lg" className="w-full" loading={busy}>
          {busy ? 'Entrando…' : (
            <>
              Entrar
              <ArrowRight className="size-4" />
            </>
          )}
        </Button>
      </form>

    </AuthLayout>
  );
}

