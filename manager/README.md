# WhatyGo Manager

Painel web do WhatyGo. React 19 + TypeScript + Vite + Tailwind CSS v4.

O servidor Go entrega o resultado do build: `manager/dist/index.html` para `/manager/*` e os
arquivos com hash em `manager/dist/assets` (ver `pkg/routes/routes.go`). O `Dockerfile` copia
`manager/dist` do repositório, então **o `dist` é versionado e precisa ser regenerado a cada mudança no painel**.

## Desenvolvimento

```bash
cd manager
npm install
npm run dev          # http://localhost:5173/manager  (proxy para VITE_API_TARGET, padrão http://localhost:8080)
npm test             # vitest (lógica pura em src/lib e src/features/*)
npm run build        # typecheck + build em manager/dist
```

Para apontar o `dev` para outro servidor: `VITE_API_TARGET=http://meu-host:8080 npm run dev`.

## Estrutura

```
src/
  api/          chamadas tipadas à API (instâncias, sessão/licença, mensagens) e o modelo de domínio
  components/
    ui/         primitivos (Button, Dialog, Menu, Switch, Tabs, Badge...), sem regra de negócio
    layout/     AppShell, Sidebar, PageHeader
  features/     uma pasta por área: auth, overview, instances, instance-detail, explorer, calls
                (calls: telefone no navegador, chamadas ao vivo, histórico; o áudio é puro em audio.ts, com testes)
  hooks/        React Query (instâncias, saúde), rascunho de formulário
  lib/          http, formatação, catálogo de eventos, cURL
  stores/       zustand: sessão (persistida em "whatygo-auth") e UI (tema, menu)
  styles.css    tokens de design (cores, sombras, animações) e tema claro/escuro
```

## Convenções

- **Cores só por token** (`bg-surface`, `text-muted`, `border-line`, `bg-brand`...), nunca hex nos componentes:
  os tokens já cobrem tema claro e escuro.
- **Diálogos e menus** usam portal próprio (não `<dialog>`), para os toasts ficarem sempre acima.
- **Catálogo de eventos** em `src/lib/events.ts` espelha `pkg/internal/event_types` no servidor; ao criar um
  evento novo no Go, inclua-o aqui.
- **Vídeo das chamadas**: o navegador codifica e decodifica H.264 com WebCodecs (`features/calls/video.ts` recebe e desenha, `video-send.ts` captura, recorta para 360×640 e codifica). O servidor não converte nada. As partes puras (NAL, SPS, giro, recorte, cadência de keyframes) têm testes; para testar o envio sem câmera, abra o Edge ou o Chrome com `--use-fake-device-for-media-stream --use-fake-ui-for-media-stream`.
- **Áudio das chamadas**: o microfone é lido por um AudioWorklet (`features/calls/capture-worklet.ts`) que o Vite
  emite como arquivo próprio em `dist/assets`; a política de segurança (`script-src 'self'`) não aceitaria um worklet
  criado na hora. O WebSocket do stream exige a origem da página em `CALL_STREAM_ORIGINS` quando ela difere da da API
  (no `npm run dev`, a origem é `localhost:5173`).
- **Estado de servidor** vive no React Query; formulários editam uma cópia (`useDraft`) que só é
  substituída por dados novos quando não há edição pendente.
- O login só valida a chave no servidor (`/instance/all`): não há etapa de licença (o fluxo herdado do upstream
  foi removido, ver `docs/LICENCA-ANALISE.md`). A sessão fica em `whatygo-auth`, no `sessionStorage`.

As fontes (Inter e JetBrains Mono, licença OFL) ficam em `src/assets/fonts`.
