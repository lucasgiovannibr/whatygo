# Propostas de features e sugestões

Vem de issues e PRs abertos no upstream e do que apareceu no teste com instância real. Cada item traz origem, esforço, risco e recomendação. Contexto geral e status de todas as issues/PRs em [FORK-TRIAGE.md](FORK-TRIAGE.md).

Escala: **Esforço** = P (poucas linhas) · M · G (dias). **Recomendação**: ▶ implementar · ◐ avaliar antes · ✖ descartar por enquanto.

## 1. Implementado no fork

Documentação dos endpoints: [`docs/wiki/guias-api/api-fork-additions.md`](docs/wiki/guias-api/api-fork-additions.md). A coluna "Ao vivo" indica o que foi exercitado com uma instância real em 29/09/2026.

| Proposta | Origem | Resultado | Ao vivo |
|---|---|---|---|
| Mídia "ver uma vez" (`viewOnce`) em `/send/media` | PR #147 | Feito (JSON e multipart; helper único + teste) | ✅ aparece "abrir uma vez" |
| `POST /message/markplayed` | Issue #45 | **Já existia** na main; issue desatualizada | — |
| `POST /message/subscribe` (presença de contato) | PR #152, issue #146 | Feito; devolve a presença ao celular após 2 min se `alwaysOnline` estiver desligado (o PR original a deixava "online" para sempre) | 🟡 responde sucesso; entrega de eventos `Presence` não observada |
| `PictureURL` em `/user/info` | PR #121 | Feito com orçamento de tempo compartilhado e 429/504 | ✅ 0,34 s |
| `POST /user/lid` | PR #179 | Feito | ✅ LID → telefone |
| `POST /user/contacts` | PR #129 | Feito, com normalização do número e `saveOnPrimaryAddressbook` opcional. **Não há remoção via API** (app state só grava) | ✅ contatos de teste criados |
| Rotas de solicitações de entrada em grupo | Issue #42 | `POST /group/requests` e `/group/requests/update`; JIDs de participantes canônicos (sem `+`) | ✅ lista vazia, validações; aprovar/rejeitar de um pedido real não testado |
| `POST /send/pollVote` | Issue #26 | Feito com `BuildPollVote`; tenta o LID próprio se o segredo da enquete estiver sob ele | ✅ voto aparece no celular |
| Status seguro do proxy + `PROXY_FAIL_CLOSED` | Issue #123 | `GET /instance/proxy/{id}`, sem credenciais; opção de nunca cair para conexão direta | 🟡 só "sem proxy"; proxy real **não testado** (decisão sua) |
| Melhoria do `passkey-helper` (1Password) | Issue #173 | WebAuthn no mundo MAIN (v1.1.0) | ❌ não testado (conta sem passkey) |
| `quoted.text` | Issue #189 | Texto do card da citação (JSON e multipart) | ✅ card mostra o texto |
| QR junto do passkey | Issue #148 | `/instance/qr` mantém `qrcode` ao lado dos campos `passkey*` | — |
| Resultado por participante em `/group/participant` | Achado no teste real | `data` por participante e `failed` (antes "success" mesmo sem adicionar) | ✅ número inexistente → `Error: 404` |
| **Diagnóstico do runtime** (`GET /instance/{id}/runtime`, `GET /instance/runtimes`) | Sugestão 23 | Estado real do processo vs banco, com avisos codificados (cliente órfão, runtime de instância apagada, "pareada no banco e dispositivo novo no runtime"...) e estatísticas do processo | ✅ 5 ciclos criar/apagar; detectou o resíduo de goroutines |
| **Health check** (`GET /health`) | Sugestão 24, issue #175 | Prontidão: ping de cada banco (2 s, em paralelo), `slow`/`error`, 503 com banco fora. `/server/ok` segue como liveness | ✅ 503 com o Postgres parado, volta a 200 sozinho |
| `ENABLE_PPROF` (`/debug/pprof`, chave global, desligado por padrão) | Necessidade do diagnóstico | Permite ver as pilhas; foi ele que mostrou o vazamento do logger | ✅ |
| Liberar o logger de instância apagada | Achado com o pprof | 1 arquivo aberto por instância criada nunca era fechado. Limite: a goroutine do `lumberjack` não pode ser parada (1 por instância já criada) | ✅ descritores estáveis em 6 ciclos |
| **Eventos `ReachoutTimelock`, `StreamError` e `ClientOutdated`** | Levantamento do whatsmeow, sugestões 30 e 31 (issues #50, #124, #115, #185, 405) | Publicados sob `CONNECTION`, no diagnóstico do runtime; o erro 463 do envio agora explica a restrição e até quando; o 405 descarta o cache da versão | ✅ testes de ponta a ponta (evento → estado → webhook → assinante); ❌ os eventos reais não puderam ser provocados (dependem de o WhatsApp restringir a conta ou recusar a versão) |
| **Eventos de pareamento** (`PairError`, `QRScannedWithoutMultidevice`, `CATRefreshError`) e **de estado de chat** (`Mute`, `Pin`, `Star`, `MarkChatAsRead`, `ClearChat`, `DeleteChat`, `DeleteForMe`, `UnarchiveChatsSetting`, `UserStatusMute`) | Levantamento do whatsmeow, sugestões 32 e 33 | Publicados sob `QRCODE`, `CONNECTION` e `CHAT_PRESENCE`; o full sync que segue um pareamento **não** é publicado | ✅ testes de ponta a ponta; 🟡 eventos reais de fixar/silenciar/estrela dependem de ação no celular (não observados) |
| **Timer de mensagens temporárias** (`POST /chat/disappearing`, `POST /user/defaultDisappearing`, aplicação automática no envio) | Issue #79, sugestão 34 | O timer é aprendido (mensagens, `EPHEMERAL_SETTING`, grupos, endpoint) e aplicado em `ContextInfo.Expiration`; grupos o releem após reiniciar; `DISAPPEARING_AUTO_APPLY=false` desliga | ✅ grupo (24h aplicado, "off" não aplica, relido após reiniciar), chat individual e padrão |
| **Grupo por convite** (`POST /group/inviteinfo`, `POST /group/joininvite`) | Sugestão 35 | Consulta por link/código ou por cartão de convite, sem entrar; entrar pelo cartão | ✅ `inviteinfo` por link do grupo de teste; `joininvite` só com testes unitários (exige um cartão real) |
| **Canais**: seguir, deixar de seguir, silenciar, marcar como visto, reagir (`/newsletter/follow` ... `/react`) | Sugestão 35 | Validação de JID (400), prazo de 20 s | 🟡 validações ao vivo; as ações em si **não** testadas (não há canal de teste) |
| **Mensagem que não chegou**: evento `UndecryptableMessage` e `POST /message/rerequest` (`BuildUnavailableMessageRequest`) | Sugestão 36 | O evento traz id/chat/sender; a rota pede o reenvio ao celular e devolve o `requestId` | ✅ pedido aceito pelo servidor; 🟡 a resposta do celular e o evento real não observados |
| **Consultas somente leitura**: `POST /user/devices`, `GET /user/statusprivacy`, `POST /user/business` | Levantamento do whatsmeow (§4 de `docs/WHATSMEOW-CAPABILITIES.md`) | Limite de 10 s; 404 para número sem perfil comercial | ✅ dispositivos (celular e linkados de dois números), privacidade do status, 404 do perfil comercial; ❌ um perfil comercial real não foi testado (não há conta Business à mão) |
| **Todos os eventos restantes do whatsmeow** (`Blocklist`, `PrivacySettings`, `BusinessName`, eventos de chamada, `MediaRetry`, eventos de canal, `OfflineSyncPreview`) | Levantamento do whatsmeow (§5 de `docs/WHATSMEOW-CAPABILITIES.md`) | Publicados sob `CONTACT`, `CALL`, `MESSAGE`, `NEWSLETTER` e `CONNECTION`; `RotateADVSecret` não vaza mais no log | ✅ testes; ✅ `Blocklist` real; ❌ os demais dependem de situações que não dá para provocar |
| **Resultado por número em `POST /message/subscribe`** (lista de até 100) | Sugestão 26, issue #146 | `data`/`failed`; a string única responde como antes | ✅ lista de dois números |
| **Presença em tempo de execução**: ligar/desligar `alwaysOnline` marca a presença e inicia/para o agendador na hora | Sugestão 27 | Trava contra dois agendadores | ✅ logs "presence now available/unavailable" |
| **`duration` em `/chat/mute`** (`8h`, `1w`, `always`, `30m`) | Achado ao corrigir as rotas de chat | Padrão continua 1 h | ✅ `always` confirmado no celular |
| **Fila de webhook por destino** (limites, descarte do mais antigo, tentativas com espera crescente, degradação, estatísticas em `/instance/runtimes`; `WEBHOOK_QUEUE_*`) | Risco achado na análise de bugs | Ver FORK-TRIAGE.md | ✅ entrega, falha e recuperação com receptor local; ❌ receptor travado e fila estourada só em teste |
| **Limpar o webhook** com `"disabled"`/`"false"` em `/instance/connect` (`""` continua "não alterar") | Pedido do teste do webhook | Evita apagar o webhook a cada reconexão do manager | ✅ |
| **Chamadas** (experimental): atender, discar, desligar, controlar o vídeo e levar áudio (PCM 16 kHz) e vídeo (H.264) por um WebSocket por chamada; `callsEnabled` por instância; `CALL_*`; eventos `CallReady`, `CallEnded`, `CallVideoState` | Proposta 20 (PR #141), reimplementado no fork sobre `purpshell/meowcaller` fixada em um commit | Opt-in por instância (a biblioteca pré-aceita toda chamada recebida), recusa instância com proxy (a mídia UDP ignoraria o proxy), bilhete de uso único no lugar da chave na URL, limites de chamadas simultâneas e por minuto, sem chamadas em grupo. Guia em [`api-call.md`](docs/wiki/guias-api/api-call.md) | ✅ em 30/09/2026 com um número real e um iPhone: áudio e vídeo recebidos e enviados, upgrade de áudio para vídeo nos dois sentidos, celular girado em várias posições (a rotação do vídeo recebido chega corrigida), vídeo em pé ocupando a tela; `enable` sem vídeo recusado (409, só nos testes); ❌ chamada em grupo e duração longa não testadas |
| **`PUT /instance/{id}/integrations`**: grava webhook, eventos e produtores sem conectar a instância | Pedido do manager | Vale na hora se a instância roda | — |
| **Painel `/manager` refeito** (React 19, TypeScript, Vite, Tailwind; tema claro e escuro; código-fonte em `manager/`, `dist` versionado) | Reconstruído no fork (PR #26) | Ver `manager/README.md` | — |
| **Swagger regenerado sem regressão** (swag v1.16.3: 116 rotas, 28 novas, nenhuma perdida; as de `/license/*` ficam declaradas em `pkg/core/license_swagger.go`) | Sugestão 29 | `make swagger` dá o mesmo resultado | ✅ gerado e conferido contra o anterior |

## 2. Propostas ainda em aberto

### Melhorias de comportamento

| # | Proposta | Origem | Esforço | Comentário | Recomendação |
|---|---|---|---|---|---|
| 11 | **Thumbnail HQ em `/send/link`** (card grande de preview) | PR [#207](https://github.com/evolution-foundation/evolution-go/pull/207) (572 linhas), issue [#103](https://github.com/evolution-foundation/evolution-go/issues/103) | M | Envolve upload de mídia de link (`MediaLinkThumbnail`); precisa de teste em aparelho | ◐ |
| 15 | **Histórico profundo no pareamento** (`HistorySyncConfig`: 10 anos / 2 GB) | parte do PR [#133](https://github.com/evolution-foundation/evolution-go/pull/133) | P | Aumenta banda/armazenamento e tempo de sync; deveria ser configurável por env, não constante | ◐ |
| 16 | **Backoff do loop de reconexão** (até 30 min de espera) | PR [#197](https://github.com/evolution-foundation/evolution-go/pull/197) | M | Evita martelar o servidor com instância deslogada, mas atrasa recuperação legítima; janela e degraus deveriam ser configuráveis | ✅ Feito no fork (PR #62), com degraus configuráveis e teto de 5 min |
| 17 | **Redesenho do ciclo de vida** (restauração no startup, `GetQr` 409, backoff com jitter) | PRs [#145](https://github.com/evolution-foundation/evolution-go/pull/145), [#154](https://github.com/evolution-foundation/evolution-go/pull/154) | G | O núcleo (um runtime por instância, canal de kill próprio, restauração de `Reconnecting`, instância apagada não reinicia) **já foi feito** de forma cirúrgica. O que sobra é opcional | ◐ |

### Features grandes (avaliar valor de produto)

| # | Proposta | Origem | Tamanho | Comentário | Recomendação |
|---|---|---|---|---|---|
| 18 | **Encaminhar mensagens** (`forward`; a lib não guarda mensagens, então precisa de conteúdo enviado ou persistência própria) | PRs [#132](https://github.com/evolution-foundation/evolution-go/pull/132) / [#150](https://github.com/evolution-foundation/evolution-go/pull/150) | ~4,9 mil linhas (base `develop`) | O PR cria os arquivos em `routes/` e `sendMessage/` **na raiz do repositório**, não em `pkg/`: é uma cópia duplicada do `send_service.go` e não integra. A ideia é boa; reimplementar enxuto em `pkg/` | ◐ |
| 19 | **Evento de agenda** `POST /send/event` | PR [#90](https://github.com/evolution-foundation/evolution-go/pull/90) | 702 linhas, base `develop` | Recurso de nicho | ✖ por ora |
| 20 | **Chamadas**: atender/discar/controlar e stream de áudio/vídeo por WebSocket | PR [#141](https://github.com/evolution-foundation/evolution-go/pull/141) | 5,2 mil linhas / 19 arquivos | O whatsmeow só oferece `RejectCall` e eventos: **não há sinalização VoIP nem mídia na lib** (conferido). O PR implementa isso por conta própria, ou seja, um subsistema novo com risco e manutenção próprios; exigiu projeto separado | ✅ Feito no fork, com implementação própria (ver §1) |
| 21 | **UI de chat no "sender"** (enviar/receber em tela) | PR [#182](https://github.com/evolution-foundation/evolution-go/pull/182) | 1,6 mil linhas | Ferramenta de teste; não é núcleo da API | ✖ por ora |
| 22 | **Manager: drawer mobile e ações visíveis em touch** | PR [#184](https://github.com/evolution-foundation/evolution-go/pull/184) | 5 arquivos de UI | Melhoria de usabilidade, baixo risco; não avaliei visualmente | ◐ (abrir o manager no celular antes de decidir) |

## 3. Sugestões novas (ainda não implementadas)

As sugestões 23 e 24 (diagnóstico do runtime e health check), 26 e 27 (resultado por item e presença em tempo de execução) 30 a 36 (eventos operacionais, de pareamento e de estado de chat, timer de mensagens temporárias, convites, canais e mensagem que não chegou) e 29 (swagger regenerado sem perder as rotas de licença) já foram feitas (§1). As de whatsmeow vêm do levantamento em [`docs/WHATSMEOW-CAPABILITIES.md`](docs/WHATSMEOW-CAPABILITIES.md).

| # | Sugestão | Por quê | Esforço | Recomendação |
|---|---|---|---|---|
| 25 | **Métricas Prometheus** (instâncias conectadas, reconexões, eventos entregues/falhos, conexões do pool) | Observar uso prolongado e alertar antes de a instância cair | M | ✅ Feito (`GET /metrics`, PR #38 em diante) |
| 28 | **Remover contato via API** | Impossível hoje: o encoder de app state do whatsmeow só gera `SET` (conferido no código). Detalhes e outros limites duros em `docs/WHATSMEOW-CAPABILITIES.md` §2 | — | ✖ (depende da lib) |
| 37 | **Erros de URL de mídia como 400** (hoje um 404 ou arquivo grande demais em `/send/media` responde 500) | Distingue erro do chamador de erro do servidor; pede separar os dois tipos em todos os fluxos de envio | P | 🟡 Parcial: erros de validação dos services são 400/404/409 (PR #72); falhas de download de URL ainda dependem de separar os tipos |
| 38 | **Atualização parcial em `POST /user/privacy`** (hoje o handler exige todos os campos) | Mudar só "visto por último" sem reenviar o resto | P | ◐ |
| 39 | **Enquetes no modo SQLite** (a tabela `poll_votes` usa `TEXT[]`, só Postgres) | Hoje `/send/pollVote` e os resultados não funcionam sem Postgres | M | ◐ |
| 40 | **Remover código morto** apontado pelo `staticcheck` (`convertToWebP`, `stringPointer`, `sectionsToString`, dois campos do repositório de instâncias, uma atribuição no `Connect`) | Manutenção | P | ✅ Feito (golangci-lint no CI, PR #68) |
| 41 | **`webhookUrl: ""` limpar o webhook** | Só depois de corrigir o manager embutido, que envia `""` em toda reconexão (o código-fonte do painel agora está em `manager/`) | M | ✖ por ora |

## 4. Documentação pendente

A documentação de tudo que está em §1 foi atualizada no preparo do release (30/09/2026): wiki (`api-call.md` reescrita, `api-instances.md`, `api-fork-additions.md`, `events-system.md`, `environment-variables.md`), CHANGELOG, swagger, FORK-TRIAGE e `docs/WHATSMEOW-CAPABILITIES.md`. Ainda em aberto:

- Botões e listas: validados em aparelho em 01/10/2026 (ver `FORK-TRIAGE.md` §3). Falta testar no Android e em grupos.
- README: instruções de Windows (PRs #201/#202 são um começo, mas duplicam o bloco "Setup").

## 5. Ideias que sobraram da análise de outubro de 2026

Não entraram na rodada de endurecimento (ver [CHANGELOG](CHANGELOG.md)); cada uma muda comportamento ou exige decisão.

| # | Ideia | Por quê | Esforço | Recomendação |
|---|---|---|---|---|
| 42 | **Licença sem reaproveitar a `GLOBAL_API_KEY`** | O runtime de licença (`pkg/core`, ofuscado, do upstream) usa a chave global como chave para o servidor de licença e a guarda em claro em `runtime_configs`. Mexer nisso é mexer no mecanismo do fornecedor | — | ✅ resolvido: o runtime de licença foi removido (ver `docs/LICENCA-ANALISE.md`) |
| 43 | **TTL e limite de tamanho nas filas RabbitMQ** | Fila sem consumidor cresce até o broker reagir. Mudar os argumentos de uma fila existente faz o broker recusar a declaração; precisa de opt-in e nota de migração (ou *policy* no broker) | M | ◐ |
| 44 | **Criptografar o proxy (e outros segredos) no banco** | Hoje fica em claro na coluna `instances.proxy`; a API já não o devolve | M | ◐ |
| 45 | **Migrações versionadas** (goose/golang-migrate) com lock | `AutoMigrate` roda a cada partida em todas as réplicas | M | ◐ |
| 46 | **Retenção de `messages`** e índice por `source`/`timestamp` | A tabela só cresce; `timestamp` é texto | M | ◐ |
| 47 | **`labels` com chave única `(instance_id, label_id)`** | `UpdateLabel` nunca atualiza e a corrida cria duplicatas | P | ◐ |
| 48 | **Fila de envio por instância** (em vez de limite + 429) | Quem manda rajadas receberia a mensagem em ordem, sem tratar 429 | G | ◐ |
| 49 | **Escrita do estado de pareamento/QR em um passo só e ownership por lease com heartbeat** | O lock atual vale enquanto a sessão do banco vive; um lease com heartbeat tolera falhas parciais de rede melhor | G | ◐ |
| 50 | **Padronizar os 500 restantes dos handlers** (`instance not found` etc.) com `apierror` e quebrar o `handleEvent` em mapa de handlers por tipo de evento | Continuidade da refatoração (restam ~640 linhas) | M | ◐ |

## 6. Melhorias das chamadas (análise de 02/10/2026)

Análise do stream de chamadas (WebSocket) contra a documentação do `meowcaller`, do Twilio Media Streams e da OpenAI Realtime. Ponto de partida: o WebSocket só existe entre o cliente e o servidor; entre o servidor e o WhatsApp a mídia vai por SRTP/UDP dentro da `meowcaller`. Trocar o WebSocket por WebRTC só ajuda quem usa navegador pela internet (o TCP trava o áudio inteiro quando um pacote se perde); para um agente de IA no mesmo servidor ou rede o WebSocket serve.

**Não conferido:** se a `meowcaller` expõe DTMF e estatísticas de RTP (perda, jitter, RTT). O código-fonte dela só existe dentro do Docker de build; vale um spike antes de planejar os itens que dependem disso.

| # | Ideia | Por quê | Esforço | Status |
|---|---|---|---|---|
| 51 | **Métricas de chamada em `/metrics`** | Não havia nenhuma. Chamadas ativas por fase, iniciadas/encerradas por direção e motivo, duração, discagens por resultado, quadros e descartes do stream, pedidos de keyframe | P | ✅ PR #75 (mesclado em 02/10/2026) |
| 52 | **Detector de mídia parada** | Os issues abertos da `meowcaller` citam "o peer manda 8 pacotes de silêncio e para" e "sem áudio de entrada". Chamada ativa sem áudio de entrada por N s gera `CallMediaStalled` (e `CallMediaResumed`); desligar é opt-in, porque um peer mudo pode não mandar pacotes (DTX) e isso foi testado ao vivo em 02/10/2026 (iPhone, chamada recebida de 68 s): falando chegam ~12 a 17 quadros/s; em silêncio, ou com o microfone mudo, o iPhone continua mandando ~2 a 3 quadros/s, nunca zero, então o limite de 15 s não disparou. A parada de verdade (a dos issues da `meowcaller`) não foi reproduzida: o disparo está só nos testes unitários | P | ✅ PR #75 (mesclado em 02/10/2026); disparo só em testes unitários |
| 53 | **`mark` como no Twilio** | Hoje só há `clear`. Com `mark` o servidor avisa quando o áudio enviado foi realmente tocado; é o que um agente de IA precisa para saber até onde falou antes de ser interrompido | P | ✅ PR #76 (mesclado em 02/10/2026); testado ao vivo: o `mark` voltou 10,0 s depois com um tom de 10 s |
| 54 | **Fila de áudio de entrada menor** | `toClientFrames = 50` guarda 3 s antes de descartar: um cliente lento acumula até 3 s de atraso. 10 a 15 quadros (600 a 900 ms) é mais próximo de conversa ao vivo | P | ✅ PR #76 (mesclado em 02/10/2026) |
| 55 | **Quadros binários no stream** (opt-in no ticket) | Base64 em JSON infla 33% e custa codificação; o JSON fica só para controle | M | ✅ PR #78 (mesclado em 02/10/2026); testado ao vivo em PCM 16 kHz |
| 56 | **Formato de áudio negociável** (`encoding` e `sampleRate` no ticket: PCM 8/16/24 kHz, μ-law e A-law 8 kHz) | A OpenAI Realtime usa 24 kHz e telefonia μ-law 8 kHz: hoje cada cliente reamostra | M | ✅ PR #77 (mesclado em 02/10/2026); testado ao vivo em μ-law 8 kHz: o eco ficou bom |
| 57 | **Duração máxima e timeout por silêncio** (opcionais) | Uma chamada atendida hoje pode durar para sempre. Estava em "não feito de propósito" | P | ✅ PR #79; testado ao vivo: `silence_timeout` e `max_duration`. Padrão desligado (`CALL_MAX_DURATION`, `CALL_SILENCE_TIMEOUT`) |
| 58 | **Timestamp nas mensagens de entrada** | Hoje só há `seq`; ajuda a sincronizar e gravar | P | ✅ PR #78 (mesclado em 02/10/2026): `timestamp` em ms em todo quadro de mídia, JSON ou binário |
| 59 | **Histórico de chamadas no banco** | Hoje só existem os eventos `CallReady`/`CallEnded`/`CallVideoState` | M | ✅ PR #80: só metadados, desligado por padrão (`CALL_HISTORY`), retenção de 90 dias (`CALL_HISTORY_RETENTION_DAYS`), `GET`/`DELETE /call/history`. Testado ao vivo com seis chamadas (recebida perdida, recusada e atendida; discada recusada, cancelada e atendida). Não testado: `unanswered` e `failed` |
| 60 | **Gravação por chamada** (`record: true`: WAV estéreo entrada/saída, vídeo `.h264`, disco ou MinIO) | Precisa de aviso de consentimento (LGPD). O áudio não chega em ritmo constante (em silêncio vêm ~2 quadros/s): quem grava precisa preencher os vazios com silêncio pelo tempo, senão o WAV sai mais curto que a chamada (o script de teste gravou 29 s de uma chamada de 68 s) | M | ✖ decidido em 02/10/2026: **não gravar chamadas** (risco legal e de privacidade); fica de fora |
| 61 | **Eventos de fala** (`speech_start`/`speech_end`, VAD por energia) | Cada cliente deixaria de implementar a interrupção | M | ✅ PR #81 (mesclado em 02/10/2026); testado ao vivo com cinco falas (início e fim batem com o áudio gravado) |
| 62 | **Página de chamadas no manager** (lista ao vivo, atender/rejeitar/desligar, softphone de teste com microfone via AudioWorklet em 16 kHz) | Funciona com o WebSocket atual, sem WebRTC; serve também de ferramenta de teste | M | ✅ PR aberto (`feat/manager-calls`): aba **Chamadas** com chamadas ao vivo, telefone no navegador (WebSocket, sem WebRTC), histórico e `callsEnabled`; testado ao vivo no navegador do desktop (atender, silenciar, desligar e ligar). **Com vídeo** (WebCodecs): receber testado com iPhone, enviar testado com a câmera falsa do Edge; o pedido de vídeo do iPhone é aceito na hora pelo painel (testado sem câmera no computador); falta câmera real, "Iniciar vídeo" e o botão manual "Aceitar vídeo" |
| 63 | **Matriz de testes ao vivo**: Android, WhatsApp Business, peer com vários aparelhos, contatos por LID, contas em coexistência | Só testamos um iPhone; os issues da `meowcaller` apontam SRTP chaveado para o aparelho errado em multi-device e aparelhos que continuam tocando depois que um atende | M | ◐ |
| 64 | **Gateway WebRTC (Pion)** para um telefone no navegador de verdade | O vídeo H.264 passa sem transcodificar. O áudio exige Opus↔PCM: o decoder da Pion é maduro, o encoder está em v0.1.0 (sem controle de bitrate); alternativas são `tphakala/go-opus` ou libopus via CGO | G | ✖ decisão sua (feature nova; só compensa para navegador pela internet) |
| 65 | **UDP por proxy SOCKS5** | Hoje instâncias com proxy ficam sem chamadas. Exige um `PacketConn` injetável na biblioteca (fork ou PR) | G | ✖ decisão sua |
| 66 | **Fork da `meowcaller`** | Pré-aceite seletivo (hoje toda chamada recebida é pré-aceita), base do whatsmeow fixa (a `main` deles migrou para o `hypermeow`) e acompanhar o Opus, que é "em andamento" lá | G | ✖ decisão sua |
| 67 | **Chamadas em grupo** | O commit fixado já traz suporte experimental; hoje fica de fora de propósito | G | ✖ por ora |

**Achado no teste da `main` combinada (02/10/2026), corrigido no PR #82:** a biblioteca pede áudio ao cliente desde o início da chamada, inclusive tocando, e o jogava fora; uma saudação enfileirada antes de o outro lado atender se perdia e o `mark` voltava como "tocado". O áudio agora espera a chamada ficar ativa.

**Documentação** de #51–#59, #61 e #82 feita em 02/10/2026: `docs/wiki/guias-api/api-call.md` (opções do stream, histórico, limites, métricas, erros), variáveis, eventos, CHANGELOG, FORK-TRIAGE e Swagger.

## 7. Propostas da segunda análise (04/10/2026)

Resultado da segunda rodada de análise (`ANALISE-SISTEMA.md` §15). Os achados corrigíveis foram
corrigidos (PRs #91 a #103); o que é funcionalidade nova fica aqui.

| # | Ideia | Por quê | Esforço | Status |
|---|---|---|---|---|
| 68 | **Assinatura HMAC dos webhooks** (`X-Webhook-Signature`) | O receptor não consegue provar que o POST veio do WhatyGo; hoje só a URL secreta protege | M | ◐ |
| 69 | **WebSocket `/ws` com token de instância** (e ticket de uso único) | Hoje só a chave global autentica, então um cliente (inquilino) não pode assinar os próprios eventos; e a chave vai na query string | M | ◐ |
| 70 | **Paginação em `GET /group/list` e `GET /user/contacts`** | `participants=false` já resolve o tamanho dos grupos; os contatos continuam vindo todos de uma vez | P | ◐ |
| 71 | **Imagem "slim" sem ffmpeg/poppler** | Quem usa só o conversor externo de áudio (`API_AUDIO_CONVERTER`) carrega ~100 MB de binários que não usa | M | ◐ |
| 72 | **Token da instância guardado como hash** (`sha256`) | Hoje fica em texto puro; exige mostrar o token uma única vez na criação | M | ◐ |
| 73 | **Validar `participant` em respostas citadas** | A checagem existe no código mas está morta; ligá-la rejeitaria citações sem `participant` que funcionam hoje em conversas 1:1 | P | ✖ só se houver caso real de citação errada |
| 74 | **Mídia recebida por URL em vez de base64 no webhook** (transmitir do arquivo temporário para o MinIO sem passar pela memória) | O teto de 50 MB limita o pior caso, mas o base64 ainda custa ~4× o arquivo por mensagem; com MinIO só os bytes são lidos (1×), sem MinIO continua o base64 | M | ◐ |
