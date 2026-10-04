# Triagem do fork — 2026-09-29

Fork: `lucasgiovannibr/whatygo` · Upstream: `evolution-foundation/evolution-go`

Atualizado em 29/09/2026, depois do teste com instância real, das propostas aprovadas e de quatro rodadas de caça a bugs (PRs #14 a #22).

Foco do fork: **corrigir, melhorar e ajustar**. O que é funcionalidade nova está em [FEATURE-PROPOSALS.md](FEATURE-PROPOSALS.md): a seção 1 lista o que **já foi implementado** (com sua aprovação) e as seções 2 e 3 o que segue **em aberto** para avaliação.

## 1. Estado do fork

- Em 29/09 a `main` do fork estava **idêntica** ao `upstream/main` (commit `9337afc`, versão `0.7.2`), então não havia o que atualizar. Em 30/09 ela está **125 commits à frente** (31 PRs mesclados dentro do próprio fork, até o #36).
- O upstream tinha **61 issues + 61 PRs abertos** (todos analisados aqui).
- A branch `upstream/develop` está *atrás* da `main` (VERSION `0.7.1`) — vários PRs abertos apontam para ela (#90, #132, #150, #159–#163, #177, #198) e por isso estão desalinhados com a `main`.
- Os commits da `main` pública são `sync: 0.7.x from main`, feitos por um bot — ela é um **espelho** de um repositório interno. O GitHub não lista nenhum PR como *merged*; os PRs #33 e #91 (AlwaysOnline) foram apenas **fechados** em 03/07 e a correção chegou à `main` por outro caminho. **O fork é o lugar prático para integrar correções.**
- A `main` do upstream exige **ativação de licença** (o servidor responde 503 `LICENSE_REQUIRED` até registrar). **Este fork removeu esse mecanismo** (ver `docs/LICENCA-ANALISE.md`).

## 2. O que foi entregue

Tudo foi enviado **somente ao fork** (`origin`). O remote `upstream` está com o push desabilitado localmente (`DISABLED_NEVER_PUSH_TO_UPSTREAM`) e `remote.pushDefault=origin`; nenhum PR foi aberto no repositório oficial.

| Onde | Situação |
|---|---|
| `main` do fork | Tudo mesclado por PRs **dentro do fork** (#1–#30 e #36): triagem, whatsmeow novo + Go 1.26, hardening, ciclo de vida, features, correções do teste real, diagnóstico, eventos do whatsmeow, mensagens temporárias, convites, canais, mensagem que não chegou, correção das rotas de chat, fila de webhook, quatro rodadas de caça a bugs, espera pela conexão, painel refeito, `PUT /instance/{id}/integrations` e chamadas (atender, discar, vídeo). CI verde; imagem em `ghcr.io/lucasgiovannibr/evolution-go` |
| Stack de teste local | `docker/fork-test/` (Postgres novo + imagem do fork, porta 8100), isolado dos seus outros containers |

Build, `go vet` e `go test -race ./...` passam.

### Correções da triagem (PRs #1–#6)

**Quedas do processo (as mais graves)**

| Problema | Issues | Correção |
|---|---|---|
| `fatal error: concurrent map writes` derruba todas as instâncias | #203, #188, #75 | `safemap` para `clientPointer`/`myClientPointer`/`killChannel` (PR #196); reconexões duplicadas da mesma instância são ignoradas; `StartClient` deixou de recursar a cada kill |
| `panic: concurrent write to websocket connection` | #99 | Produtor WebSocket reescrito: escrita serializada por conexão, deadline, conexão morta é removida e **vários assinantes por instância** (PR #181). Testes com `-race` |
| Panic em `events.Archive` | #95, #101 | Payload montado explicitamente (havia type assertion em valor cru) |

**Vazamento de conexões Postgres** (`too many clients already`) — #106 #109 #112 #118 #165 #175 #186

`StartClient` chamava `sqlstore.New` a cada (re)conexão e nunca fechava. Agora existe **um** container por processo, reaproveitando o `authDB` que já tem pool limitado (25/5). Erro de criação não é memorizado. Substitui 9 PRs concorrentes.

**Configuração e segurança**

| Problema | Issues | Correção |
|---|---|---|
| `/instance/connect` zera eventos/RabbitMQ/flags | #111 | Atualização parcial (PR #136) |
| `advanced-settings`: "acesso negado" **e** qualquer token de instância mexia nas configs de outra | #81 | Novo `AuthInstanceScoped`: chave global **ou** token da própria instância |
| SQL montado com `fmt.Sprintf` a partir do corpo da requisição | — | `ForceUpdateJid` parametrizado (a rota é admin, mas era injeção) |
| `Disconnect` apagava as assinaturas de eventos | — | PR #187 |

**Mensagens e eventos**

| Problema | Issues | Correção |
|---|---|---|
| `/group/participant` sempre 400 | #97, #52 | Validador de array correto (PR #180) |
| Edição de mensagem chega criptografada | #62, #92, #146 | Decrypt **antes** da troca LID→PN (a chave depende do JID original). Base: PR #153 |
| Enquete: resultados sempre 404 | #60 | Mesma causa: o voto era descriptografado depois do swap LID→PN, falhava e nunca era gravado |
| Eventos `Passkey*` nunca chegam ao webhook | #105 | Seguem a assinatura `QRCODE` |
| `PICTURE`/`USER_ABOUT`/`BUTTON_CLICK` aceitos mas nunca publicados no NATS | #193 | Mapeamento único `globalEventTypeFor()` para AMQP e NATS (eram dois `switch` copiados) |
| mentionAll + documento/mídia falha | #114 | PR #137 |
| Carrossel: botões URL/CALL/COPY sem parâmetros; JSON inválido com aspas | #51 | `buildCarouselButton` com `json.Marshal`; aceita `url`, `phoneNumber` e `COPY_CODE` |
| Imagem enviada sem `Width/Height` (bolha quadrada) | #104, #25 | Dimensões enviadas no `ImageMessage` |
| Sticker animado falha / re-encode desnecessário | — | PR #166 (WebP passa direto, download limitado) |

**Endpoints**

| Problema | Issues | Correção |
|---|---|---|
| `/user/avatar` trava 75s | #76 | JID canônico + IQ de 8s (PR #120); mesmo bug ao editar/apagar mensagem (PR #130) |
| `/user/profileName` pendura | #176 | Usava `SetGroupName` com JID vazio; agora patch de app-state de push name |
| `/send/text` leva ~80s para dizer "não conectado" | #77 | Instância não pareada falha imediatamente |
| `GetQr` reiniciava sessão já logada | #85 | PR #149 |
| History-sync request ia para o contato | — | Enviado como peer message ao próprio JID (PR #133) |

**Conexão e presença**

| Problema | Issues | Correção |
|---|---|---|
| Conexão "zumbi" (keepalive falha para sempre) | #185 | Reinicia após 3 `KeepAliveTimeout` (PR #126) |
| Versão do WhatsApp nunca chegava ao handshake | — | `store.SetWAVersion` (PR #199). `WHATSAPP_VERSION_*` não tinha efeito real |
| Goroutine de presença antiga enviando `available` | #55, #54, #70 | Encerra ao ser substituída ou com `alwaysOnline=false` |
| `AppStateSyncError` (LTHash) | #72 | Recovery controlado (PR #144) |
| NATS conectava sem URL | — | PR #143 |
| Mensagens não descriptografáveis | — | `REREQUEST_FROM_PHONE` (opt-in, padrão desligado — PR #156). Além disso, o evento `UndecryptableMessage` é publicado e `POST /message/rerequest` pede o reenvio (ver "Mensagem que não chegou") |
| Sem CI | — | `.github/workflows/ci.yml`: build, vet, `test -race` |

### Correções e melhorias das features e do teste real (PRs #7–#10)

Features aprovadas (PR #7), documentadas em `docs/wiki/guias-api/api-fork-additions.md`: `viewOnce`, `POST /send/pollVote`, `POST /message/subscribe`, `POST /user/lid`, `POST /user/contacts`, `PictureURL` em `/user/info`, `POST /group/requests[/update]`, `GET /instance/proxy/{id}` + `PROXY_FAIL_CLOSED`, `passkey-helper` 1.1.0.

**Bugs que só apareceram ao testar com uma instância real** (não estavam nas issues do upstream ou eram a causa raiz delas):

| Descoberta | Correção |
|---|---|
| O manager chama `POST /instance/connect` e ~2 s depois `GET /instance/qr`; o `GetQr` **iniciava a instância de novo**. Dois runtimes: o duplicado não pareado girava QR até o máximo, forçava logout e reiniciava a instância como dispositivo novo **logo depois do pareamento bem-sucedido** (explica boa parte de #85/#148/#186 e do PR #145) | Um runtime por instância (`runtime_slot.go`): um segundo start é ignorado; o canal de kill pertence ao runtime; `ReconnectClient` espera o anterior terminar |
| Instância **apagada** pela API continuava reiniciando em loop de QR, conectando ao WhatsApp: o `Delete` fecha o canal de kill e o supervisor tratava "fechado" como "reiniciar" | Canal fechado = parar de vez; restart só com `true` e só se a instância ainda existir; a rotação de QR para quando o cliente não é mais o vigente |
| `POST /user/info` dava timeout de 10 s: JID com `+` (mesma classe do avatar, e o #76 já citava `/user/info`) | JID canônico; agora 0,34 s, com `PictureURL` e `LID` |
| `POST /group/participant` respondia "success" mesmo quando o número **não** foi adicionado (resultado por participante descartado) | Devolve `data` por participante e `failed` |
| Voto de enquete de quem só tem `@lid` gravava os dígitos do LID como telefone | Telefone real resolvido pelo store de LID |
| `viewOnce` e `quoted.text` só existiam no envio JSON | Também no multipart de `/send/media` |
| Não há como remover um contato salvo (app state só grava) | `saveOnPrimaryAddressbook` opcional em `POST /user/contacts` para testar sem tocar na agenda do celular |

### Diagnóstico e saúde (PR #12) e levantamento do whatsmeow

| Entrega | Detalhe |
|---|---|
| `GET /health` (readiness) | Ping de cada banco (2 s, em paralelo): `ok` / `slow` / `error`; 503 quando um banco cai. `/server/ok` continua liveness. A rota já era isentada pela barreira de licença, mas não existia (404). Validado ao vivo: 503 com o Postgres parado e retorno a 200 sozinho |
| `GET /instance/{id}/runtime`, `GET /instance/runtimes` | Estado real do processo vs banco, com avisos codificados e estatísticas (goroutines, memória). Detecta o estado do bug do runtime duplicado ("pareada no banco, dispositivo novo no runtime") e runtimes de instâncias apagadas |
| `ENABLE_PPROF` (opcional) | `/debug/pprof` atrás da chave global. Mostrou que cada instância criada deixava um logger com arquivo aberto e uma goroutine do `lumberjack` |
| Logger de instância apagada liberado | Descritores de arquivo estáveis em 6 ciclos criar/apagar. A goroutine do `lumberjack` não pode ser parada (limite da biblioteca): 1 por instância já criada até o processo reiniciar |
| `docs/WHATSMEOW-CAPABILITIES.md` | Do `Client` do whatsmeow (136 métodos) o projeto usava 65; dos 75 tipos de evento tratava 42 (números da época do levantamento; desde então foram tratados os eventos operacionais, de pareamento e de estado de chat). Lista os **limites duros** (remover contato, atender/discar chamadas, encaminhar por ID, ler o timer de temporárias) e os eventos não tratados que explicam issues abertas: `NotifyAccountReachoutTimelock` (o 463), `StreamError` (#185), `ClientOutdated` (405) |

### Eventos operacionais do whatsmeow (PR #13)

`NotifyAccountReachoutTimelock`, `StreamError` e `ClientOutdated` deixaram de cair no "Unhandled event": são publicados sob `CONNECTION`, aparecem no diagnóstico do runtime, e o erro 463 do envio agora explica a restrição e até quando. O 405 descarta o cache da versão (1 h) para a reconexão seguinte buscar a atual. **Não foi possível provocar os eventos reais**: dependem de o WhatsApp restringir a conta ou recusar a versão. O que foi verificado são testes de ponta a ponta com eventos sintéticos (evento → estado → webhook → assinante `CONNECTION`), mais o `go test -race` completo.

### Eventos de pareamento e de estado de chat (PR #14)

- `PairError`, `QRScannedWithoutMultidevice` (assinatura `QRCODE`) e `CATRefreshError` (`CONNECTION`) deixaram de cair no "Unhandled event": quem pareia passa a ter retorno quando o pareamento falha.
- `Mute`, `Pin`, `Star`, `MarkChatAsRead`, `ClearChat`, `DeleteChat`, `DeleteForMe`, `UnarchiveChatsSetting` e `UserStatusMute` (mudanças feitas em **outro aparelho**) são publicados sob `CHAT_PRESENCE`, onde o `Archive` já estava. Os eventos do **full sync** que segue um pareamento não são publicados (seriam milhares de "mudanças" que não são mudanças).
- **Verificado**: testes de ponta a ponta com eventos sintéticos. **Não observado**: eventos reais de fixar/silenciar/estrela (dependem de ação no celular).

### Mensagens temporárias, convites de grupo e canais (PR #14)

| Entrega | Detalhe |
|---|---|
| **#79 — timer de mensagens temporárias** | O timer de cada chat é aprendido (`ContextInfo.Expiration` recebido, `EPHEMERAL_SETTING`, `GroupInfo`, o próprio endpoint) e aplicado no envio. Grupos o releem após reiniciar. Novas rotas `POST /chat/disappearing` e `POST /user/defaultDisappearing`. `DISAPPEARING_AUTO_APPLY=false` desliga. Limite: num chat individual o timer só é conhecido depois de chegar uma mensagem dele ou de ser definido por aqui |
| Grupo por convite | `POST /group/inviteinfo` (link/código ou cartão de convite, **sem entrar**) e `POST /group/joininvite` (a partir do cartão) |
| Canais | `POST /newsletter/follow`, `/unfollow`, `/mute`, `/markviewed`, `/react`, com prazo de 20 s |

**Validado ao vivo** (grupo de teste só com você e o seu próprio chat): timer 24h aplicado, "off" não aplicado, timer relido após reiniciar o container, chat individual e padrão; `inviteinfo` por link; validações (400). **Não testado ao vivo**: as ações de canal (sem canal de teste) e `joininvite` (precisa de um cartão de convite real).

### Mensagem que não chegou (PR #15)

O evento `UndecryptableMessage` (antes só uma linha de log) é publicado sob `MESSAGE` com `id`, `chat` e `sender`, e `POST /message/rerequest` pede ao celular uma nova cópia (`BuildUnavailableMessageRequest`); a resposta chega como `Message` com `UnavailableRequestID`. **Validado ao vivo**: o pedido é aceito pelo servidor. **Não observado**: a resposta do celular e um evento real (dependem de o WhatsApp entregar algo que não dá para decifrar).

### Rotas de chat, presença e `subscribe` em lote (PR #17)

| Problema | Correção |
|---|---|
| `/chat/pin`, `unpin`, `archive`, `unarchive`, `mute`, `unmute` (marcadas "TODO: not working" no router, #101) não faziam efeito em contatos | A causa era dupla: `utils.ParseJID` põe um `+` no número, então o patch de app state mirava `+<número>@s.whatsapp.net`, um chat que **não existe** (a loja até criava uma linha para ele), e o celular indexa chats individuais por **LID** (67 dos 68 chats da conta de teste). Agora o JID é canônico e resolvido para o LID (`utils.AppStateChatJID`). Em grupo nunca falhou, por isso parecia intermitente. Entrada inválida dá 400 (era 500), o `timestamp` devolvido é real (era data zerada) e `/chat/mute` aceita `duration` opcional (`8h`, `1w`, `always`, `30m`; padrão continua 1 h). **Confirmado no celular**: chat fixado e silenciado apareceram |
| `POST /message/subscribe` só aceitava um número | Aceita lista (até 100) com resultado por número (`data`/`failed`); a string única responde como antes (proposta 26) |
| Ligar `alwaysOnline` em tempo de execução não tinha efeito até reconectar | Marca a presença e inicia/para o agendador na hora, com trava contra um segundo agendador (proposta 27) |

### Rodadas de caça a bugs (PRs #18, #19 e #22)

Como foram achados: `staticcheck` e leitura de **todo** ponto que monta JID (`ParseJID`/`CreateJID`) ou faz HTTP de saída. Cada correção tem teste; as marcadas ✅ foram exercitadas com a instância real.

| Problema | Correção | Ao vivo |
|---|---|---|
| `/user/block` e `/unblock`: o `+` no JID fazia a lib esperar um timeout de usync | JID canônico; 1,2 s | ✅ bloqueou e desbloqueou o contato de teste |
| `GET /group/myall` (marcada "not working") sempre vazia: comparava o dono do grupo (LID) com o próprio JID mal montado | Compara o usuário do LID e do telefone | ✅ 6 grupos (antes 0) |
| Rótulos de chat e de mensagem: mesmo bug de app state do pin | Mesmo helper (canônico + LID) | ✅ criar, rotular, remover |
| Rótulo apagado no celular continuava na tabela local e em `/label/list` | Apaga ao receber `LabelEdit` com `Deleted` | ✅ |
| `/community/add` e `/remove`: listas de sucesso/falha montadas errado (todo grupo ia para "success") e grupo inválido enviado como JID vazio | Função única `applyToGroups` | só testes (não há comunidade) |
| HTTP de saída sem timeout (mídia por URL, preview de link, busca da versão do WhatsApp Web — que segura um mutex por onde passa todo start de instância —, webhooks) | Clientes com timeout de conexão, de cabeçalho e total | testes com servidores que travam |
| Instância sem JSON de proxy quebrava `/instance/connect` e o start do cliente quando há proxy global no ambiente | `parseProxyConfig` trata `""`/`null` como "sem proxy" | só testes (proxy não ativado, por decisão sua) |
| `/send/link`: página ilegível, `og:image` relativa ou imagem ausente derrubavam o envio; valores enviados pelo chamador eram sobrescritos pelos da página; `url` ignorada; título de SVG vencia o do documento; ponto final entrava no link | Preview "melhor esforço"; o que o chamador enviou prevalece; `url` respeitada; `og:title` ou primeiro `<title>`; pontuação final fora do link | ✅ link com ponto final enviado |
| Mídia por URL: página de erro (404) era enviada como o arquivo e o tamanho era ilimitado | `utils.DownloadBytes`: rejeita não-2xx e limita (100 MB mídia, 20 MB imagens, 2 MB miniatura); falha em imagem de carrossel/botão vai para o log | ✅ 404 → erro claro |
| Conversão de áudio com ffmpeg sem limite de tempo | 3 minutos | só testes |
| `/send/poll` sem validação (`maxAnswer` maior que as opções virava "ilimitado" em silêncio) | 2 a 12 opções, sem vazia/repetida, `maxAnswer` dentro do número de opções | ✅ 400 |
| `/send/location` rejeitava latitude ou longitude 0 (Equador, Greenwich) | Só o par (0,0) conta como ausente; faixas conferidas | ✅ latitude 0 enviada |
| `SendMessage` buscava o cliente 11 vezes; instância parada no meio do envio deixava um nil | Uma busca, validada | só testes |
| **Apagar instância não conectada deixava o dispositivo no banco para sempre** (chaves de sessão, identidade e grupo, milhares de contatos em cache, mapa de LID) e os votos de enquete nunca eram apagados | Apagar remove o dispositivo guardado e os votos (o celular ainda lista a sessão: remover por lá) | ✅ votos; dispositivo testado com um store SQLite real |
| `poll_votes` único por (enquete, votante) sem a instância: duas instâncias no mesmo grupo se sobrescreviam | Único por (instância, enquete, votante); migração idempotente | ✅ constraint trocada no banco |
| `/user/check` respondia 200 com `data: null` quando a consulta falhava | Devolve o erro (400 número inválido, 429/504 limite e timeout) | ✅ 400 |
| `/message/delete` devolvia data zerada; `/user/contacts` dava `null` sem contatos e ordem diferente a cada chamada | Data real; lista vazia; ordem estável | ✅ 2.501 contatos, mesma ordem |

**Resíduo do bug antigo do pin**: a linha `+553197157574@s.whatsapp.net` (fixada) continua no estado do WhatsApp e no banco de teste; o celular a ignora e não há rota para removê-la.

### Fila de webhook por destino (PR #20) e limpar o webhook (PR #21)

- **Antes**: uma goroutine por evento, cada uma com até 5 tentativas de 30 s. Com o receptor fora do ar sob tráfego intenso, goroutines e payloads (mídia inclusa) se acumulavam sem limite.
- **Agora**: uma fila por URL, limitada em eventos (`WEBHOOK_QUEUE_MAX_EVENTS`, 1000) e em bytes (`WEBHOOK_QUEUE_MAX_MB`, 64), com no máximo `WEBHOOK_QUEUE_WORKERS` trabalhadores (4; 1 mantém a ordem estrita) que só existem enquanto há trabalho. Fila cheia descarta o evento **mais antigo** e conta. Tentativas com espera de 1 s, 5 s, 30 s e 2 min com aleatoriedade; depois que um evento esgota as tentativas o destino fica "degradado" e os eventos seguintes têm uma tentativa cada, até um passar. A mesma URL do webhook global não recebe o evento duas vezes.
- **Observabilidade**: `GET /instance/runtimes` traz o bloco `webhook` (destinos, degradados, pendentes, em envio, bytes, enviados, falhos, descartados).
- **Validado ao vivo** com um receptor local: entrega normal, receptor em falha (3 eventos em envio com nova tentativa) e recuperação sem perda. **Só em teste unitário**: receptor travado e estouro da fila.
- **Limpar o webhook**: `webhookUrl: ""` em `/instance/connect` continua "não alterar" (o manager embutido envia `""` em toda reconexão; tratar como "limpar" apagaria o webhook a cada reconexão, o bug do #111 de volta). `"disabled"` ou `"false"` limpam (guardado vazio; um `"disabled"` antigo continua ignorado na entrega). Validado ao vivo, inclusive a reconexão no estilo do manager.

### Eventos restantes do whatsmeow (PR #24)

Todos os eventos que a lib entrega ao handler estão tratados (70 dos 75 tipos; os outros 5 nunca chegam sozinhos, ver `docs/WHATSMEOW-CAPABILITIES.md` §5). Novos: `Blocklist`, `PrivacySettings`, `BusinessName` (`CONTACT`), `CallPreAccept`, `CallTransport`, `CallReject`, `UnknownCallEvent` (`CALL`), `MediaRetry` (`MESSAGE`), `NewsletterLiveUpdate`, `NewsletterMuteChange` (`NEWSLETTER`) e `OfflineSyncPreview` (`CONNECTION`).

- **Achado no caminho**: `RotateADVSecret` caía na linha genérica "Unhandled event", que imprime o evento inteiro com `%+v`: o segredo ADV **antigo e o novo** da sessão iam parar no log da instância. Agora só há um aviso de que houve rotação; teste garante que os valores não aparecem no log.
- **Verificado**: testes (mapeamento, publicação pelo handler, roteamento nas duas listas de assinatura, conteúdo do log). **Ao vivo**: `Blocklist` chegou de verdade ao bloquear e desbloquear o contato de teste (dois eventos) e nenhuma linha "Unhandled event" apareceu. **Não observados**: os demais (dependem de chamadas, mídia a reenviar, mudança de privacidade em outro aparelho ou fila offline).

### Espera pela conexão e consultas (PR #25)

| Problema / entrega | Correção | Ao vivo |
|---|---|---|
| Nove serviços iniciavam a instância e dormiam 2 s fixos antes de checar a conexão (3 s depois de reconectar, 3 s + 2 s no `GET /instance/qr`, 2 s no `ForceReconnect`): uma conexão de 2,1 s falhava a requisição e uma de 0,3 s ainda custava 2 s | `utils.WaitForClient` (usa `WaitForConnection`): responde quando conecta, limite de 10 s; instância sem dispositivo pareado falha na hora (#77); `GetQr` espera QR, passkey ou login | ✅ instância sem pareamento falha em 0,47 s; QR em 0,35 s (antes 3 s fixos); envio depois de desconectar em 0,97 s |
| `POST /user/devices`, `GET /user/statusprivacy`, `POST /user/business` (novos, só leitura) | 10 s de limite; 404 quando o número não é Business | ✅ ver PROPOSALS |

### Painel, `integrations` e chamadas (PRs #26, #27, #28–#30 e #36)

- **Painel `/manager`** (#26): refeito do zero (React 19, TypeScript, Vite, Tailwind). O código-fonte passou a estar no repositório (`manager/`); o `dist` continua versionado porque o Dockerfile o copia.
- **`PUT /instance/{id}/integrations`** (#27): grava webhook, eventos e produtores sem conectar a instância.
- **Chamadas** (#28 fundação e limites, #29 discar, #30 vídeo, #36 correções do teste ao vivo): reimplementação do PR #141 do upstream. Decisões: biblioteca `purpshell/meowcaller` fixada no último commit compatível com o whatsmeow do projeto (`6d9b7b2c1807`; a `main` dela migrou para outro fork do whatsmeow); **opt-in por instância** (`callsEnabled`, porque a biblioteca pré-aceita toda chamada recebida); **instância com proxy recusada** (a mídia UDP ignoraria o proxy e vazaria o IP); **bilhete de uso único** no WebSocket em vez da chave na URL; limites de chamadas simultâneas e por minuto; motivo de fim e eventos próprios (`CallReady`, `CallEnded`, `CallVideoState`). Guia em `docs/wiki/guias-api/api-call.md`.
- **Achados do teste ao vivo** (30/09/2026, número real e iPhone), corrigidos no #36:
  - O WhatsApp não manda motivo quando o outro lado desliga; o `CallEnded` saía com motivo vazio e agora sai `peer_hangup`.
  - O valor de rotação do vídeo recebido (extensão RTP) conta no sentido **anti-horário**, ao contrário do que a biblioteca documenta; o stream entrega os giros horários que deixam a imagem em pé. A orientação do `video_state` é a do aparelho e não serve para girar.
  - `video_state` não distinguia "aceitou o upgrade" de "desligou a câmera"; ganhou `state` e `stateCode` (o iPhone manda um código 2 sem nome logo após atender, significado não confirmado).
  - `enable` sem vídeo prévio: o iPhone ignora; agora responde 409 e manda usar `start`.
  - Um teste dependia da ordem entre o fechamento do `Done` e a publicação do `CallEnded` e falhava de vez em quando.
- **Não feito de propósito**: chamadas em grupo e recuperação de um stream caído depois do prazo. (A duração máxima agora existe, desligada por padrão: ver abaixo.)

### Melhorias das chamadas (PRs #75–#82, 02/10/2026)

Análise do stream de chamadas contra a biblioteca, o Twilio Media Streams e a API Realtime da OpenAI (itens #51–#67 de `FEATURE-PROPOSALS.md` §6). Feito e **testado ao vivo** (número real e iPhone), um PR por item:

- **Stream**: formato de áudio escolhido no bilhete (#77), quadros binários e `timestamp` (#78), `mark` e fila de entrada de 900 ms (#76), eventos de fala (#81). Correção achada no teste da `main` combinada (#82): o áudio enfileirado enquanto o telefone toca era descartado pela biblioteca; agora espera a chamada ficar ativa.
- **Vigilância e limites**: `CallMediaStalled`/`CallMediaResumed` (#75) e `CALL_MAX_DURATION`/`CALL_SILENCE_TIMEOUT` (#79), todos opt-in ou só avisam. Uma parada real de áudio (a dos issues da biblioteca) **não foi reproduzida**: só testes unitários.
- **Histórico** (#80): só metadados, desligado por padrão, com retenção de 90 dias. **Gravar chamadas ficou de fora, por decisão**: risco legal e de privacidade.
- **Métricas** `evolution_call*` (#75, #80).
- **Achados**: um aparelho mudo ou em silêncio continua mandando 2 a 3 quadros por segundo (por isso o áudio de entrada não é contínuo e quem grava precisa posicionar pelo `timestamp`); o WhatsApp informa `rejected` quando o outro lado recusa uma chamada discada.
- **Painel** (#62): aba **Chamadas** na instância, com as chamadas ao vivo, o telefone no navegador (atender, ligar e falar com o microfone da página), o histórico e o interruptor `callsEnabled` em Comportamento. Testado ao vivo no navegador do desktop. **Vídeo no navegador**: receber foi testado com um iPhone e enviar com a câmera falsa do Edge (o computador de teste não tem câmera): o iPhone mostrou a imagem em pé, preenchendo a tela, e o servidor contou 258 imagens em 16 s, nenhuma descartada. O pedido de vídeo do iPhone é **aceito na hora** pelo painel (o iPhone retira o pedido em poucos segundos; aceito em 0,5 s o vídeo veio, em 6 s não), testado sem câmera no computador: 107 imagens. Não testados: câmera real, "Iniciar vídeo" numa chamada de voz e o botão manual "Aceitar vídeo".
- **Ficou de fora**: testes em outros aparelhos (#63: só um iPhone foi testado), gateway WebRTC, UDP por proxy, fork da biblioteca e chamadas em grupo (#64–#67, decisões suas).

### Validação ao vivo (instância real, 29/09/2026)

Instância pareada e conectada por você; mensagens só para o seu próprio número; grupo de teste só com você (o primeiro foi removido no fim; o "ZZ Teste Timer Fork", criado depois para as mensagens temporárias, ainda existe e deve ser apagado à mão). As validações das rodadas seguintes estão nas tabelas de cada PR acima.

| Verificado | Resultado |
|---|---|
| Pareamento por QR, restauração no restart, presença com `alwaysOnline=false` | OK |
| Texto, citação com `quoted.text`, enquete, `pollVote` | OK (confirmado no celular) |
| Voto recebido do celular (#60) e edição recebida (#62) | OK: decifrados e gravados; `/polls/{id}/results` retorna o voto |
| `viewOnce` (aparece "abrir uma vez") e `width`/`height` | OK |
| `/user/info`, `/user/lid`, avatar, `/message/subscribe` | OK (0,3 s; avatar antes 75 s) |
| `profileName` (alterado e restaurado), `POST /user/contacts` | OK; contatos de teste criados (ver abaixo) |
| Grupo: criar, listar, info, nome, descrição, link, 8 ações de `/group/settings`, `requests`, `participant`, `mentionAll` com texto/imagem/documento, sair | OK; cada ação de `settings` foi conferida em `/group/info` |
| `advanced-settings` com token de outra instância / próprio / chave global | 403 / 200 / 200 |
| Criar instância nova: `connect`+`qr` em sequência e simultâneos | um único runtime |
| Apagar instância que estava girando QR | sem nenhuma atividade depois |
| Postgres | 3 conexões estáveis; **zero** panics em toda a sessão |

**Contatos de teste que ficaram na lista do WhatsApp** (remover à mão, a API não remove): "ZZ Teste Fork" (553100000001) e "ZZ Teste Fork 2" (553196596774).

**Não testado**: proxy (por decisão sua), passkey (a conta não exigiu), ações de canal, `joininvite`, comunidades, eventos reais de estado de chat de outro aparelho e de mensagem indecifrável, receptor de webhook travado ou fila estourada, redes instáveis. **Uso prolongado**: medição de 48 h em andamento (amostra a cada 10 min: saúde, goroutines, memória, conexões do Postgres, reinícios, panics); ela reinicia a cada troca de imagem, e o último início foi às 00:28 UTC de 30/09.

## 2.1 Rodada de endurecimento (outubro de 2026, PRs #38–#72 do fork)

Não veio de issues do upstream: veio de uma análise completa do sistema (segurança, escalabilidade, memória, velocidade de envio, retorno de erros), com medição antes e depois. O texto completo está no [CHANGELOG](CHANGELOG.md) ("Hardening and scale round") e a documentação em `docs/wiki`.

| Fase | PRs | O que mudou |
|---|---|---|
| 1 — higiene e correções rápidas | #38–#45 | dependências e `govulncheck`; token fora do payload; limites de corpo, CORS e timeouts do servidor; chave global fraca recusada; métricas e `X-Request-ID`; logs de instância apagada; `instance_id` em `messages`; produtores independentes; `WADEBUG`/`LOGTYPE` |
| 2 — caminho de envio e recebimento | #46–#52 | cache do usync; eventos sem assinante não são montados; corpo lido uma vez e editado no lugar; logger assíncrono; cache de `GetGroupInfo`; retries sem refazer download |
| 3 — rede e dados | #53–#60 | SSRF; MinIO sem política pública e por instância; limites de imagem e semáforo no ffmpeg; imagem sem root; senha do proxy fora das respostas; atualizações pontuais no banco e pool configurável; escritor de mensagens em lote |
| 4 — escala e resiliência | #61–#67 | RabbitMQ/NATS robustos; parada ordenada, backoff de reconexão e partida escalonada; limite de envio com 429; corridas; mídia em trabalhadores; `disconnect` de verdade e `ClientProvider`; uma réplica por instância |
| 5 — engenharia | #68–#72 | CI com gofmt, lint, manager e Trivy (achou o CVE-2023-4863 no webp); erros tipados com `code`; manager em `sessionStorage` e CSP; `handleEvent` de 1.300 para 640 linhas |

Ficou de fora de propósito: o acoplamento da chave global com o mecanismo de licença (`pkg/core`, código ofuscado do upstream) e tudo que exige a conta real para validar (latência de `/send/*`, mídia recebida pelo caminho novo, `disconnect`, segunda réplica).

## 3. Botões e listas (#59 #71 #110 #170 #204) — corrigido (botões, carrossel), lista como botões

Era o grupo mais reportado (5 issues) e ficou sem correção enquanto não havia aparelho. Em 01/10/2026 foi testado ao vivo com uma conta **WhatsApp Business** conectada como aparelho vinculado, enviando para um **iPhone** e para o **WhatsApp Web** (o Android não foi testado). Regra que apareceu: **o `200` só diz que o servidor aceitou; quem mostra se renderiza é o cliente**, que descarta em silêncio o que não entende (a web mostra "não foi possível carregar a mensagem").

Método: campo temporário `variant` no endpoint, cada formato enviado de verdade e conferido no aparelho (o código de experimento não foi commitado).

| Mensagem | O que o WhatsApp aceitou e renderizou |
|---|---|
| Botões reply, copiar, URL, ligar, agrupados, reply com imagem | `InteractiveMessage` simples com native flow, **sem** o wrapper `DocumentWithCaptionMessage`, anunciado com `<biz><interactive type="native_flow" v="1"><native_flow v="9" name="mixed"/></interactive></biz>` + `<bot biz_bot="1"/>`. O `v="9"` e o `name="mixed"` são o que importa |
| O que falhava | `ButtonsMessage` legado: sempre `405`. CTA com `native_flow` sem `v="9"` ou com o nome do botão: `473`/`405`. Com o wrapper o celular mostra, a web não |
| Reply + CTA misturados | aparece no celular, **não** na web (o servidor passou a aceitar a mistura) |
| Pix | já funcionava (wrapper + `payment_info`); só no celular |
| Carrossel | sumia **só no iPhone** (a web mostrava): cada card mandava `title`/`subtitle` do cabeçalho como string vazia. Omitir os campos vazios resolveu; `<biz>` não é necessário (só acrescenta o selo "IA") |
| Lista (`ListMessage` legado, `single_select`, com ou sem wrapper, com várias combinações de `<biz>`) | **sempre recusada** (`405`/`479`) ou aceita e descartada pelo celular. Não existe nesse tipo de sessão, nem em Business |

- **Lista**: `/send/list` tenta a lista e, se ela for recusada, reenvia como **botões de resposta** (3 por mensagem, até 9 itens; o `rowId` vira o `id` do botão e o toque chega como `ButtonClick`). `fallbackButtons: false` devolve o `502 whatsapp_rejected` explicado. Conferido: 1 e 2 mensagens, e cada toque chegou ao servidor com o `rowId`.
- A hipótese "o bump do whatsmeow resolve" já tinha sido descartada (a lógica de `<biz>` da lib é idêntica nas duas versões); o que resolveu foi o formato da mensagem.
- O #110 estava certo sobre o wrapper; o #204 acertou que o nó `<biz>` era a questão, mas o conserto é o `native_flow v="9"`, não deixar a lib gerar o nó (ela só cobre `ButtonsMessage`/`ListMessage`, que são justamente os recusados).

## 3.1 Atualização do whatsmeow (já na `main` do fork)

O projeto estava fixo no whatsmeow de 30/06; o novo (29/09) são **72 commits**. Os que importam para as issues abertas: `client: ensure stream error is handled before reconnecting` e `don't reuse handler queue between connections` (#185, #190), `user: update IsOnWhatsApp query` + `fix parsing not on whatsapp responses` (#32), `send: always use LID for DMs`, `message: handle stateless pkmsgs correctly`, tokens de privacidade em LID (#50/#124) e vários updates de protobuf.

Verificado: build, `go vet`, `go test -race`, boot com Postgres, imagem Docker (Go 1.26) e teste de integração do pool. **Atenção**: o schema do whatsmeow passa de v14 para **v16** (duas migrações só de ida). Depois de subir, **não dá para voltar à imagem antiga** no mesmo banco — faça backup do `evogo_auth` antes.

## 4. Limites da validação

- **Testado ao vivo** com uma única instância (conta pessoal, chat consigo mesmo e um grupo só com você). Não foram exercitados (nessa rodada): proxy, passkey, ações de canal, conta Business, número de terceiros, tráfego intenso, quedas de rede e uso prolongado.
- Cada rodada passa por `go build`, `go vet`, `go test -race ./...` (inclui testes novos e um teste de integração do pool Postgres, opt-in) e boot com Postgres.
- O Go não está instalado na máquina; a compilação roda em containers `golang:1.26`.

## 5. Situação atual, o que ainda falta e sugestões

**Feito e mesclado no fork**: triagem completa; correções (§2); whatsmeow novo (Go 1.26); CI; publicação da imagem no GHCR; hardening; ciclo de vida; features aprovadas; correções do teste real; diagnóstico e health check; eventos operacionais, de pareamento e de estado de chat; mensagens temporárias (#79); convites de grupo; canais; mensagem que não chegou. rotas de chat corrigidas; fila de webhook; quatro rodadas de caça a bugs. CHANGELOG, wiki e estes documentos atualizados até o PR #22.

**Ainda falta (e por quê)**

| Item | Motivo |
|---|---|
| Erro 463 em contatos frios (#50 #124) | Depende de observação prolongada com o whatsmeow novo |
| Uso prolongado (horas/dias) | **Em andamento**: medição de 48 h com a instância ativa; só vale a partir da última troca de imagem, então convém não mexer no código até terminar |
| PRs #145/#154/#191/#192 (e #197, feito no PR #62 do fork) | O ponto central (um runtime por instância, restauração no startup, goroutines órfãs, backoff da reconexão) foi tratado de forma cirúrgica; o restante depende de decisão/teste ao vivo |
| #32 (número fixo "not registered") | O whatsmeow novo corrige o parse do `IsOnWhatsApp`; reavaliar com um número fixo |
| #69, #107 | Parecem comportamento do WhatsApp; sem causa no código |
| Proxy real e passkey | Não testados |
| Enquetes em SQLite | A tabela `poll_votes` usa `TEXT[]` (Postgres); no modo SQLite as enquetes não funcionam |

**Sugestões**: tudo o que veio do levantamento do whatsmeow e foi aprovado **já foi feito** (diagnóstico, health check, eventos operacionais, pareamento, estado de chat, timer, convites, canais e `rerequest`). Também feitos: resultado por item em lote (`subscribe`), presença em tempo de execução e a fila de webhook. Em aberto, em FEATURE-PROPOSALS.md §2 e §3: métricas Prometheus, regenerar o swagger, thumbnail HQ em `/send/link` (#103), encaminhar mensagens e os itens que dependem de decisão de produto.

**Para você**: deixar a instância de teste rodando e me dizer se aparecer algo estranho nos logs; decidir sobre proxy/passkey quando quiser testá-los; escolher o que sobrou em FEATURE-PROPOSALS.md; remover à mão o grupo "ZZ Teste Timer Fork" e os contatos de teste.

## 6. Resumo numérico

**Issues (61)**

| Status | Qtde |
|---|---|
| ✅ Corrigido | 37 |
| 🟡 Parcial / validar | 6 |
| 🟣 Depende do whatsmeow | 2 |
| 📝 Proposta | 1 |
| 🔵 Já na main | 6 |
| 🔍 Investigar | 3 |
| 🔁 Duplicada | 4 |
| ⚪ Sem ação de código | 2 |

**Pull requests (61)**

| Decisão | Qtde |
|---|---|
| ✅ Aplicado / Reimplementado | 23 |
| ⏩ Superado (duplicado ou coberto) | 23 |
| 📝 Proposta (feature) | 5 |
| ⏸ Não aplicado | 7 |
| 🟡 Parcial | 3 |

Legenda de PRs: **Aplicado** = mesclado (com adaptações ao `safemap`); **Reimplementado** = mesma ideia, código próprio; **Superado** = outro PR/implementação já cobre; **Parcial** = só parte foi usada.

## 7. Todas as issues

| # | Título | Status | Nota |
|---|---|---|---|
| [#20](https://github.com/evolution-foundation/evolution-go/issues/20) | Erro 400 ao consultar /instance/status após desconectar instância manualmente | 🔵 Já na main | Corrigida no 0.7.2 (mantenedor confirmou); só está aberta até a release. Pode fechar. |
| [#21](https://github.com/evolution-foundation/evolution-go/issues/21) | /instance/pair returns empty PairingCode despite success message | 🔵 Já na main | Corrigida no 0.7.2 (Pair engolia o erro do `PairPhone`). Pode fechar. |
| [#25](https://github.com/evolution-foundation/evolution-go/issues/25) | The uploaded image will only appear after clicking to download it. | ✅ Corrigido | Thumbnail JPEG já existia na main; agora `Width`/`Height` também vão no `ImageMessage` (mesma causa do #104). **Validado ao vivo** (instância real, 29/09). `width`/`height` presentes no payload. |
| [#26](https://github.com/evolution-foundation/evolution-go/issues/26) | feat: add /send/pollVote endpoint to programmatically vote on polls | ✅ Corrigido | `POST /send/pollVote` implementado. **Validado ao vivo** (instância real, 29/09). Voto enviado do fork e aparece no celular; erro claro para enquete desconhecida; em grupo exige `participant`. |
| [#32](https://github.com/evolution-foundation/evolution-go/issues/32) | erro ao enviar mensagem para numero fixo que tem whatsapp | 🔍 Investigar | Erro "not registered" com número fixo. Workaround: `"formatJid": false`. Causa exata (normalização do 9º dígito / `+` no `IsOnWhatsApp`) não reproduzida. |
| [#42](https://github.com/evolution-foundation/evolution-go/issues/42) | UpdateGroupSettings function exists but no route is registered | 🔵 Já na main | `POST /group/settings` **já existe** na main (ações `announcement`, `not_announcement`, `locked`, `unlocked`, `approval_on/off`, `admin_add`, `all_member_add`). Documentada no wiki (`api-groups.md`). O swagger não foi regenerado (o `swag init` gera diff enorme e remove as rotas de licença). Rotas de *request participants* seguem sem rota (ver propostas). **Validado ao vivo** (instância real, 29/09). As 8 ações mudam o estado (conferido em `/group/info`); `/group/description` e nome também. |
| [#45](https://github.com/evolution-foundation/evolution-go/issues/45) | [Feature] Novo endpoint POST /message/markplayed (microfone azul em áudios) | 🔵 Já na main | `POST /message/markplayed` **já existia** na main (issue desatualizada). |
| [#50](https://github.com/evolution-foundation/evolution-go/issues/50) | Error 463 (NackCallerReachoutTimelocked) — tctoken/cstoken not persisted afte… | 🟣 Depende do whatsmeow | tctoken/cstoken. Depende do bump do whatsmeow (branch `deps/whatsmeow-bump`). Relato de produção em comentário indica que **463 não vai a zero** só com o bump. |
| [#51](https://github.com/evolution-foundation/evolution-go/issues/51) | [BUG] Carousel message buttons (URL, CALL, COPY_CODE) lose their parameters d… | ✅ Corrigido | Carrossel: params dos botões agora são JSON válido; aceita `url`, `phoneNumber` e `COPY_CODE` (o payload do relato usava esses nomes e era ignorado). |
| [#52](https://github.com/evolution-foundation/evolution-go/issues/52) | /group/participant - "participants is required and cannot be empty" bug | 🔁 Duplicada | Duplicada de #97 (corrigida). |
| [#54](https://github.com/evolution-foundation/evolution-go/issues/54) | [Bug] Notifications on APP was gone after connected to EvoGo | 🔁 Duplicada | Duplicada de #55. |
| [#55](https://github.com/evolution-foundation/evolution-go/issues/55) | AlwaysOnline=false não é respeitado — instância fica online permanentemente | 🟡 Parcial / validar | A main já respeita `alwaysOnline` no `Connected`. Corrigido: goroutine de presença antiga (uma por reconexão) continuava enviando `available`; agora encerra ao ser substituída ou com `alwaysOnline=false`. Validar no caminho de reconexão relatado. **Validado ao vivo** (instância real, 29/09). Após reiniciar: "Marked self as unavailable (alwaysOnline=false)". Falta observar por horas/dias. |
| [#59](https://github.com/evolution-foundation/evolution-go/issues/59) | Button and List messages do not render on consumer WhatsApp — only Carousel w… | ✅ Corrigido | Botões, carrossel e Pix renderizam (verificado em aparelho); a lista é enviada como botões de resposta, porque o WhatsApp recusa lista de aparelho vinculado. Ver "Botões e listas". |
| [#60](https://github.com/evolution-foundation/evolution-go/issues/60) | GET /polls/{pollMessageId}/results always returns 404 "No votes found" even a… | ✅ Corrigido | Voto de enquete era descriptografado **depois** da troca LID→PN, então sempre falhava e nada era gravado (404). Agora descriptografa antes. **Validado ao vivo** (instância real, 29/09). Voto feito no celular foi descriptografado e gravado; `/polls/{id}/results` retorna o voto. `voterPhone` agora traz o telefone real, não os dígitos do LID. |
| [#62](https://github.com/evolution-foundation/evolution-go/issues/62) | Eventos de edição de mensagem não estão sendo entregues corretamente | 🔁 Duplicada | Duplicada de #92 (corrigida). **Validado ao vivo** (instância real, 29/09). Log: "Decrypted edited message ... targeting ...". |
| [#69](https://github.com/evolution-foundation/evolution-go/issues/69) | /send/carousel splits message into two bubbles (Text + Cards) instead of send… | 🔍 Investigar | O código já coloca `body`/`footer` dentro do `InteractiveMessage` (não há envio de texto separado). Se ainda aparecem 2 balões, é comportamento do cliente WhatsApp — precisa de teste em aparelho. |
| [#70](https://github.com/evolution-foundation/evolution-go/issues/70) | Não chega notificações no celular depois de conectado | 🟡 Parcial / validar | Mesmo grupo do #55 (presença). Caso extra: WhatsApp Business no iPhone. Validar após o fix de presença. |
| [#71](https://github.com/evolution-foundation/evolution-go/issues/71) | Testei todos os botões/listas, retorna 200 mas nenhum chega (exceto carrossel) | 🔁 Duplicada | Duplicada de #59 (resolvida junto). |
| [#72](https://github.com/evolution-foundation/evolution-go/issues/72) | Erro 500 ao arquivar conversa em sessões WhatsApp Android (LTHash mismatch) -… | 🟡 Parcial / validar | `AppStateSyncError` (LTHash mismatch): aplicado o recovery controlado do PR #144. Problema de fundo é do whatsmeow/estado do app; validar. |
| [#75](https://github.com/evolution-foundation/evolution-go/issues/75) | Quando rodo dois worflows juntos quebra uma das instancias e retorna erro 500 | 🟡 Parcial / validar | Duas instâncias em paralelo quebrando com 500: provável race nos maps compartilhados / leak de pool (ambos corrigidos). Sem log para confirmar. |
| [#76](https://github.com/evolution-foundation/evolution-go/issues/76) | /user/avatar | ✅ Corrigido | `/user/avatar` com timeout de 75s: JID com `+` + IQ sem limite. Corrigido (PR #120). **Validado ao vivo** (instância real, 29/09). Avatar em 0,35 s (antes 75 s). |
| [#77](https://github.com/evolution-foundation/evolution-go/issues/77) | O endpoint /send/text demora muito tempo para retornar o erro de dispositivo/… | ✅ Corrigido | Instância não pareada agora falha na hora (antes ~80s de reconexão/retry aninhados). |
| [#79](https://github.com/evolution-foundation/evolution-go/issues/79) | Outgoing messages not respecting chat's disappearing-messages timer → recipie… | ✅ Corrigido | Timer de mensagens temporárias aprendido por chat e aplicado no envio; `POST /chat/disappearing` e `POST /user/defaultDisappearing`. **Validado ao vivo** (grupo de teste e chat individual). |
| [#81](https://github.com/evolution-foundation/evolution-go/issues/81) | instance/advanced-settings não funciona | ✅ Corrigido | Além do "acesso negado" (a rota só aceitava token de instância, não a chave global), havia falha de segurança: qualquer token de instância lia/alterava as configs de outra. Corrigido com `AuthInstanceScoped`. **Validado ao vivo** (instância real, 29/09). Token de outra instância = 403; próprio token e chave global = 200. |
| [#85](https://github.com/evolution-foundation/evolution-go/issues/85) | QRCode is not beign generated | ✅ Corrigido | QR não gera após desconectar: leak de conexões Postgres (corrigido) + `GetQr` reiniciando sessão logada (corrigido). Validar. **Causa raiz achada no teste real**: `connect` seguido de `qr` iniciava dois runtimes; o duplicado forçava logout e reiniciava como dispositivo novo logo após parear. Corrigido (um runtime por instância) e validado com chamadas em sequência e simultâneas. |
| [#92](https://github.com/evolution-foundation/evolution-go/issues/92) | Missing information on message webhook when editing/deleting a message | ✅ Corrigido | Edição de mensagem recebida agora é descriptografada e entregue como `protocolMessage` de edição, com `IsEdit=true`. **Validado ao vivo** (instância real, 29/09). |
| [#95](https://github.com/evolution-foundation/evolution-go/issues/95) | panic: "interface conversion: interface {} is *events.Archive, not map[string… | ✅ Corrigido | Panic `*events.Archive` corrigido (payload montado explicitamente). |
| [#97](https://github.com/evolution-foundation/evolution-go/issues/97) | POST /group/participant always returns 400 "participants is required and cann… | ✅ Corrigido | `/group/participant` usava o validador de string única para o array `participants`. Corrigido. **Validado ao vivo** (instância real, 29/09). A rota responde; além disso `/group/participant` agora devolve o resultado por participante (antes dizia "success" mesmo sem adicionar). |
| [#98](https://github.com/evolution-foundation/evolution-go/issues/98) | Feature Request: Group Settings Update Endpoint | 🔵 Já na main | Duplicada de #42 — `POST /group/settings` já existe. |
| [#99](https://github.com/evolution-foundation/evolution-go/issues/99) | Panic: concurrent write to websocket connection when sending WebSocket events… | ✅ Corrigido | Panic "concurrent write to websocket connection": escrita agora serializada por conexão, com deadline. |
| [#101](https://github.com/evolution-foundation/evolution-go/issues/101) | Bug: API /chat/archive - interface conversion: interface {} is *events.Archiv… | ✅ Corrigido | Mesmo panic do #95. Observação: `/chat/archive` usa o campo `chat` (não `number`). As rotas de chat eram marcadas "not working" porque o JID de contato ia errado para o app state; corrigido no PR #17 (ver "Rotas de chat"). |
| [#103](https://github.com/evolution-foundation/evolution-go/issues/103) | /send/link only produces a small link preview — high-res thumbnail (MediaLink… | 📝 Proposta | Card grande de link preview. PR #207 propõe; ver propostas. |
| [#104](https://github.com/evolution-foundation/evolution-go/issues/104) | [GOWS/send] /send/media omits imageMessage width/height → square placeholder … | ✅ Corrigido | `Width`/`Height` agora enviados no `ImageMessage`. **Validado ao vivo** (instância real, 29/09). |
| [#105](https://github.com/evolution-foundation/evolution-go/issues/105) | Passkey* events never reach the webhook: missing cases in subscription filter… | ✅ Corrigido | Eventos `Passkey*` agora seguem a assinatura `QRCODE` no webhook e nas filas globais. |
| [#106](https://github.com/evolution-foundation/evolution-go/issues/106) | Postgres connection leak: each (re)connect on a logged-out instance leaks an … | ✅ Corrigido | Leak de pool Postgres: um único container/pool reaproveitado (usa o `authDB` já limitado). **Validado ao vivo** (instância real, 29/09). 3 conexões no Postgres com a instância ativa, estável após reinícios e criação/exclusão de instâncias. |
| [#107](https://github.com/evolution-foundation/evolution-go/issues/107) | Passkey ceremony stuck at awaiting_confirmation — server never sends PairPass… | 🔍 Investigar | Passkey preso em `awaiting_confirmation` em conta Business. Comportamento do servidor WhatsApp; sem causa no código identificada. |
| [#109](https://github.com/evolution-foundation/evolution-go/issues/109) | PostgreSQL connection leak: idle connections accumulate and exhaust max_conne… | ✅ Corrigido | Leak de pool Postgres (mesma correção do #106). |
| [#110](https://github.com/evolution-foundation/evolution-go/issues/110) | Bug: Error 473 when using /send/button with type: "copy" | ✅ Corrigido | Corrigido: o CTA (`copy`/`url`/`call`) vai sem o wrapper, com `native_flow v="9" name="mixed"`. Ver "Botões e listas". |
| [#111](https://github.com/evolution-foundation/evolution-go/issues/111) | Instance settings silently reset to defaults (rabbitmqEnable, events, flags) … | ✅ Corrigido | `/instance/connect` zerava eventos/RabbitMQ/flags. Agora é atualização parcial (PR #136). |
| [#112](https://github.com/evolution-foundation/evolution-go/issues/112) | PostgreSQL connections are not released after QR/reconnect failures | ✅ Corrigido | Leak de pool Postgres (mesma correção do #106). |
| [#113](https://github.com/evolution-foundation/evolution-go/issues/113) | Missing endpoint/support for group announcement mode (open/close group settin… | 🔵 Já na main | Duplicada de #42 — `POST /group/settings` já existe. |
| [#114](https://github.com/evolution-foundation/evolution-go/issues/114) | mentionAll + media: Sending media with mentionAll causes failure / requires s… | ✅ Corrigido | mentionAll + mídia: documento com legenda vive em `DocumentWithCaptionMessage` (nil pointer). Corrigido (PR #137). **Validado ao vivo** (instância real, 29/09). Texto, imagem e documento com legenda + `mentionAll` em grupo. |
| [#115](https://github.com/evolution-foundation/evolution-go/issues/115) | /send/text is not working the message isn't being sent. | ⚪ Sem ação de código | Comentário indica que o contato precisa mandar mensagem primeiro (erro 463, ver #50). |
| [#118](https://github.com/evolution-foundation/evolution-go/issues/118) | PostgreSQL connection leak: a new sqlstore pool is created on instance reconn… | ✅ Corrigido | Leak de pool Postgres (mesma correção do #106). |
| [#123](https://github.com/evolution-foundation/evolution-go/issues/123) | feat(instance): expose safe proxy configuration and runtime status | ✅ Corrigido | `GET /instance/proxy/{id}` (status em tempo de execução, sem credenciais) e `PROXY_FAIL_CLOSED`. Não testado com proxy real (fora do escopo por decisão sua): só o status "sem proxy" foi verificado. |
| [#124](https://github.com/evolution-foundation/evolution-go/issues/124) | Error 463 permanent on cold sends for instances paired before v0.7.2 — NCT sa… | 🟣 Depende do whatsmeow | NCT salt não preenchido em instâncias pareadas antes da 0.7.2. Comentário de outro usuário discorda da causa. Depende do bump do whatsmeow; validar. |
| [#146](https://github.com/evolution-foundation/evolution-go/issues/146) | Expose SubscribePresence (contact online) + decrypt edited messages (new text… | ✅ Corrigido | Edição descriptografada + `POST /message/subscribe` (presença) implementados. **Validado ao vivo** (instância real, 29/09). Edição validada; `subscribe` responde sucesso (entrega de eventos `Presence` não observada). |
| [#148](https://github.com/evolution-foundation/evolution-go/issues/148) | QR Code Genetration not working ( version 0.7.2) | ✅ Corrigido | `/instance/qr` agora devolve também `qrcode`/`code` junto dos campos de passkey quando existir QR. Que a conta exija passkey é comportamento do WhatsApp. |
| [#165](https://github.com/evolution-foundation/evolution-go/issues/165) | Postgres connection leak: StartClient creates a new sqlstore.Container per (r… | ✅ Corrigido | Leak de pool Postgres (mesma correção do #106). |
| [#170](https://github.com/evolution-foundation/evolution-go/issues/170) | '[Bug] /send/list and /send/button fail with "server returned error 405" — le… | ✅ Corrigido | Botões corrigidos (formato novo). `/send/list` continua recusada pelo WhatsApp, então reenvia como botões de resposta; `fallbackButtons:false` devolve o `502` explicado. Ver "Botões e listas". |
| [#172](https://github.com/evolution-foundation/evolution-go/issues/172) | Problem with passcode(webauthn) | ⚪ Sem ação de código | Pedido de uso do passkey helper; ver #173 (melhoria da extensão) e docs de passkey. |
| [#173](https://github.com/evolution-foundation/evolution-go/issues/173) | Passkey Helper: 1Password/WebAuthn fails from content script — working soluti… | ✅ Corrigido | Extensão `passkey-helper` 1.1.0: WebAuthn no mundo MAIN (gerenciadores de senha funcionam). Não validado ao vivo (a conta de teste não exigiu passkey). |
| [#175](https://github.com/evolution-foundation/evolution-go/issues/175) | Postgres connection pool leak on every StartClient/reconnect cycle | ✅ Corrigido | Leak de pool Postgres (mesma correção do #106). |
| [#176](https://github.com/evolution-foundation/evolution-go/issues/176) | POST /user/profileName hangs indefinitely (no response) on latest image | ✅ Corrigido | `POST /user/profileName` chamava `SetGroupName` com JID vazio (IQ que ninguém responde). Agora usa o patch de app-state de push name. **Validado ao vivo** (instância real, 29/09). `profileName` em 0,8 s; nome alterado e restaurado. |
| [#185](https://github.com/evolution-foundation/evolution-go/issues/185) | Cliente morre após stream:error <ack class="status" type="media"/> e nunca re… | 🟡 Parcial / validar | Cliente morto após `stream:error` desconhecido. Coberto por: KeepAlive recovery (PR #126) + whatsmeow novo no branch `deps/whatsmeow-bump`. |
| [#186](https://github.com/evolution-foundation/evolution-go/issues/186) | QR Code stop generating untill I restart the docker container, Postgres error | ✅ Corrigido | Consequência do leak de pool (`too many clients`). Corrigido. |
| [#188](https://github.com/evolution-foundation/evolution-go/issues/188) | Panic (nil pointer) in ReconnectClient kills the whole process - one instance… | ✅ Corrigido | Panic nil pointer em `ReconnectClient`: maps agora seguros + reconexão duplicada da mesma instância é ignorada. |
| [#189](https://github.com/evolution-foundation/evolution-go/issues/189) | 'quoted' reply renders an empty, non-tappable quote card ('QuotedMessage' har… | 🟡 Parcial / validar | Novo campo opcional `quoted.text` preenche o card da citação. Sem ele continua vazio (o conteúdo original não é guardado). Preencher automaticamente exigiria persistir mensagens. **Validado ao vivo** (instância real, 29/09). Card da citação mostra o texto quando `quoted.text` é enviado. |
| [#193](https://github.com/evolution-foundation/evolution-go/issues/193) | PICTURE / USER_ABOUT / BUTTON_CLICK are accepted in NATS_GLOBAL_EVENTS but ne… | ✅ Corrigido | `PICTURE`, `USER_ABOUT` e `BUTTON_CLICK` agora publicados no NATS/AMQP (mapeamento único). |
| [#203](https://github.com/evolution-foundation/evolution-go/issues/203) | 0.7.2: fatal error: concurrent map writes in whatsmeowService.StartClient (ki… | ✅ Corrigido | `fatal error: concurrent map writes`: maps compartilhados agora protegidos (`safemap`) + fim da recursão do `StartClient`. |
| [#204](https://github.com/evolution-foundation/evolution-go/issues/204) | Correção de botões e Lista Resolvido | ✅ Corrigido | A causa era o formato do `<biz>`: o conserto é `native_flow v="9" name="mixed"` num `InteractiveMessage` simples (a lib só gera `<biz>` para `ButtonsMessage`/`ListMessage`, os recusados). Ver "Botões e listas". |

## 8. Todos os pull requests

| # | Título | Autor | Decisão | Nota |
|---|---|---|---|---|
| [#90](https://github.com/evolution-foundation/evolution-go/pull/90) | feat(send): add POST /send/event endpoint | NeritonDias | 📝 Proposta | `POST /send/event` (702 linhas, base `develop`). Ver propostas. |
| [#102](https://github.com/evolution-foundation/evolution-go/pull/102) | fix(whatsmeow): reuse a single capped sqlstore container (fix connection leak) | Ay0rus | ⏩ Superado | Leak de pool. Coberto pela implementação própria do container compartilhado. |
| [#117](https://github.com/evolution-foundation/evolution-go/pull/117) | fix(whatsmeow): reuse a single capped sqlstore container (fixes Postgres conn… | guilhermeCassettari | ⏩ Superado | Leak de pool. Boa ideia (pool limitado), mas o teste importa o módulo antigo `EvolutionAPI/...` e não compilaria. Substituído. |
| [#120](https://github.com/evolution-foundation/evolution-go/pull/120) | fix(user): resolve /user/avatar info query timeout via canonical JID | cesar-carlos | ✅ Aplicado | Mesclado: avatar com JID canônico, IQ de 8s, mapeia 504/429, espera cliente pronto. |
| [#121](https://github.com/evolution-foundation/evolution-go/pull/121) | feat(user): return PictureURL on POST /user/info | cesar-carlos | ✅ Reimplementado | `PictureURL` em `/user/info`. **Validado ao vivo** (0,34 s; o JID canônico faltava e foi corrigido no teste). |
| [#122](https://github.com/evolution-foundation/evolution-go/pull/122) | fix: decrypt inbound message edits and clarify revoke webhooks | cesar-carlos | ⏩ Superado | Decrypt de edição. Sua observação (decrypt **antes** da troca LID→PN) foi incorporada. |
| [#125](https://github.com/evolution-foundation/evolution-go/pull/125) | fix: panic on Archive event due to unchecked type assertion | FlavioPulli | ⏩ Superado | Panic Archive — coberto. |
| [#126](https://github.com/evolution-foundation/evolution-go/pull/126) | fix: recover zombie connections on KeepAliveTimeout | FlavioPulli | ✅ Aplicado | Mesclado (adaptado): reinicia após 3 `KeepAliveTimeout`; eventos publicados em `CONNECTION`. |
| [#127](https://github.com/evolution-foundation/evolution-go/pull/127) | fix: guard shared instance maps with a RWMutex | FlavioPulli | ⏩ Superado | Maps + mutex — coberto pelo #196. |
| [#128](https://github.com/evolution-foundation/evolution-go/pull/128) | feat: decrypt secret-encrypted message edits | FlavioPulli | ⏩ Superado | Decrypt de edição — coberto (base no #153). |
| [#129](https://github.com/evolution-foundation/evolution-go/pull/129) | feat: POST /user/contacts — save a contact to the device addressbook | FlavioPulli | ✅ Reimplementado | `POST /user/contacts`, com normalização do número e `saveOnPrimaryAddressbook` opcional. **Validado ao vivo**; a API não remove contatos (app state só grava). |
| [#130](https://github.com/evolution-foundation/evolution-go/pull/130) | fix: canonicalize JIDs in GetAvatar, DeleteMessageEveryone and EditMessage | FlavioPulli | 🟡 Parcial | Aplicada a parte de `EditMessage`/`DeleteMessageEveryone` (JID canônico); avatar veio do #120. |
| [#131](https://github.com/evolution-foundation/evolution-go/pull/131) | fix(whatsmeow): reuse shared sqlstore.Container instead of recreating it on e… | wellpelomundo | ⏩ Superado | Leak de pool — coberto. |
| [#132](https://github.com/evolution-foundation/evolution-go/pull/132) | Feat: Inclusão endpoint Encaminhamento de mensagens (forward) | iagocotta | 📝 Proposta | Encaminhar mensagens (4,9 mil linhas, base `develop`). Os arquivos foram criados em `routes/` e `sendMessage/` na **raiz** (não em `pkg/`): é uma cópia duplicada e não integra como está. Ver propostas. |
| [#133](https://github.com/evolution-foundation/evolution-go/pull/133) | fix(history): deliver on-demand history-sync and deepen on-link backfill | nicolasnovis | 🟡 Parcial | Aplicado: history-sync como peer message para o próprio JID. **Não** aplicada a config de histórico de 10 anos/2 GB (mudança de comportamento). |
| [#135](https://github.com/evolution-foundation/evolution-go/pull/135) | fix(events): serialize websocket writes per connection | cesar-carlos | ✅ Reimplementado | Websocket com escrita serializada — implementação própria (também cobre #181), com testes `-race`. |
| [#136](https://github.com/evolution-foundation/evolution-go/pull/136) | fix(instance): stop Connect/advanced-settings from wiping config | cesar-carlos | ✅ Aplicado | Mesclado: `/instance/connect` e advanced-settings passam a ser parciais. |
| [#137](https://github.com/evolution-foundation/evolution-go/pull/137) | fix(send): handle document-with-caption and @lid JIDs in mentionAll | cesar-carlos | ✅ Aplicado | Mesclado: mentionAll com documento+legenda e JIDs @lid. |
| [#141](https://github.com/evolution-foundation/evolution-go/pull/141) | feat: answer/dial/control WhatsApp calls and stream their audio/video over We… | RamonBritoDev | ✅ Reimplementado | Atender/discar/controlar chamadas + stream de áudio/vídeo por WebSocket, reimplementado no fork (não o PR, que tinha apikey na URL, `CheckOrigin` sempre verdadeiro, registro que não limpava ao desconectar e rotas de grupo/reação/tela só como esboço): biblioteca `purpshell/meowcaller` fixada em um commit, opt-in por instância (`callsEnabled`), recusa instância com proxy, bilhete de uso único, limites de chamadas, sem grupo. Testado ao vivo (30/09/2026). Ver `docs/wiki/guias-api/api-call.md`. |
| [#142](https://github.com/evolution-foundation/evolution-go/pull/142) | Eflowchat pg fix | soyezeok | ⏩ Superado | Idêntico ao #117. |
| [#143](https://github.com/evolution-foundation/evolution-go/pull/143) | fix: skip NATS connection when URL is empty | joldmarfilho | ✅ Aplicado | Mesclado: não conecta ao NATS sem `NATS_URL`. |
| [#144](https://github.com/evolution-foundation/evolution-go/pull/144) | fix: recover app-state sync errors safely | joldmarfilho | ✅ Aplicado | Mesclado: recovery de `AppStateSyncError` (validar ao vivo). |
| [#145](https://github.com/evolution-foundation/evolution-go/pull/145) | fix(instance): prevent duplicate runtimes and harden QR lifecycle  | joldmarfilho | ⏸ Não aplicado | Redesenho do ciclo de vida (1 runtime por instância, backoff, testes). Grande e sobrepõe #196/#154; requer teste ao vivo. Ver "Próximos passos". |
| [#147](https://github.com/evolution-foundation/evolution-go/pull/147) | feat(send): support view-once media on /send/media | nicolasnovis | ✅ Reimplementado | `viewOnce` em `/send/media` (JSON e multipart). **Validado ao vivo**: aparece "abrir uma vez" no celular. |
| [#149](https://github.com/evolution-foundation/evolution-go/pull/149) | fix(instance): prevent GetQr from disconnecting active logged-in session | iagocotta | ✅ Reimplementado | `GetQr` não reinicia sessão já logada. |
| [#150](https://github.com/evolution-foundation/evolution-go/pull/150) | Fix(chat)  enviar history sync request como peer para o próprio jid, não para… | iagocotta | ⏩ Superado | Contém o mesmo código do #132 + fix de history-sync (aplicado via #133). |
| [#151](https://github.com/evolution-foundation/evolution-go/pull/151) | fix(sticker): send animated WebP stickers as-is (skip static re-encode) | nicolasnovis | ⏩ Superado | Sticker animado — substituído pelo #166 (mais completo). |
| [#152](https://github.com/evolution-foundation/evolution-go/pull/152) | feat(presence): expose POST /message/subscribe to receive contact presence | nicolasnovis | ✅ Reimplementado | `POST /message/subscribe`; devolve a presença ao celular após 2 min se `alwaysOnline` estiver desligado. Responde sucesso ao vivo. |
| [#153](https://github.com/evolution-foundation/evolution-go/pull/153) | fix(message): decrypt secretEncryptedMessage MESSAGE_EDIT envelopes | Caio-HD | ✅ Aplicado | Mesclado (base do decrypt de edição), chamada movida para antes do swap LID→PN. |
| [#154](https://github.com/evolution-foundation/evolution-go/pull/154) | fix: stabilize WhatsApp connections and restore paired sessions on startup | member3541 | ⏸ Não aplicado | Estabilidade de conexão + restaurar sessões no startup + 409 no `GetQr`. Grande (678/306), sobrepõe outros; requer teste ao vivo. |
| [#156](https://github.com/evolution-foundation/evolution-go/pull/156) | Re-request undecryptable messages from the phone instead of dropping them | 6justgotme | ✅ Aplicado | Mesclado: `REREQUEST_FROM_PHONE` (opt-in, padrão desligado). |
| [#159](https://github.com/evolution-foundation/evolution-go/pull/159) | fix: panic on Archive event due to unchecked type assertion | FlavioPulli | ⏩ Superado | Panic Archive (base `develop`) — coberto. |
| [#160](https://github.com/evolution-foundation/evolution-go/pull/160) | fix: recover zombie connections on KeepAliveTimeout | FlavioPulli | ⏩ Superado | Igual ao #126 (base `develop`). |
| [#161](https://github.com/evolution-foundation/evolution-go/pull/161) | feat: decrypt secret-encrypted message edits | FlavioPulli | ⏩ Superado | Igual ao #128 (base `develop`). |
| [#162](https://github.com/evolution-foundation/evolution-go/pull/162) | feat: POST /user/contacts — save a contact to the device addressbook | FlavioPulli | ⏩ Superado | Igual ao #129 (base `develop`). |
| [#163](https://github.com/evolution-foundation/evolution-go/pull/163) | fix: canonicalize JIDs in GetAvatar, DeleteMessageEveryone and EditMessage | FlavioPulli | ⏩ Superado | Igual ao #130 (base `develop`). |
| [#166](https://github.com/evolution-foundation/evolution-go/pull/166) | fix(sticker): send WebP stickers as-is instead of re-encoding them | FlavioPulli | ✅ Aplicado | Aplicado: WebP enviado sem re-encode, download limitado, `IsAnimated`. Testes adicionados. |
| [#167](https://github.com/evolution-foundation/evolution-go/pull/167) | fix: guard shared instance maps with a RWMutex | FlavioPulli | ⏩ Superado | Maps + mutex (2ª versão) — coberto pelo #196. |
| [#174](https://github.com/evolution-foundation/evolution-go/pull/174) | fix(whatsmeow): reuse pooled authDB connection in StartClient instead of leak… | fmedeiros95 | ⏩ Superado | Leak de pool — coberto. |
| [#177](https://github.com/evolution-foundation/evolution-go/pull/177) | fix(whatsmeow): prevent panic when handling *events.Archive | dev-guidolin | ⏩ Superado | Panic Archive — coberto. |
| [#178](https://github.com/evolution-foundation/evolution-go/pull/178) | fix(whatsmeow): reuse pooled authDB connection in StartClient (fixes #175) | wilsonborba | ⏩ Superado | Leak de pool — coberto. |
| [#179](https://github.com/evolution-foundation/evolution-go/pull/179) | feat(user): add endpoint to resolve phone number from LID | fmedeiros95 | ✅ Reimplementado | `POST /user/lid`. **Validado ao vivo** (LID → telefone). |
| [#180](https://github.com/evolution-foundation/evolution-go/pull/180) | fix(group): use array-aware validation for participants | netoduwe | ✅ Aplicado | Aplicado: fix de uma linha em `/group/participant`. |
| [#181](https://github.com/evolution-foundation/evolution-go/pull/181) | fix(websocket): deliver events to every subscriber of an instance | prakash-dev-code | ✅ Reimplementado | Websocket com vários assinantes por instância — incluído na reescrita do produtor. |
| [#182](https://github.com/evolution-foundation/evolution-go/pull/182) | feat(sender): add chat-style UI for sending and receiving messages | prakash-dev-code | 📝 Proposta | UI de chat no "sender" (1,6 mil linhas). Ver propostas. |
| [#184](https://github.com/evolution-foundation/evolution-go/pull/184) | fix(manager): drawer mobile + acoes visiveis no touch | douglasanpa | 📝 Proposta | Manager: drawer mobile + ações visíveis em touch. Só UI, não avaliei visualmente. |
| [#187](https://github.com/evolution-foundation/evolution-go/pull/187) | fix(instance): do not clear event subscriptions on disconnect | guipratiko | ✅ Aplicado | Aplicado: `Disconnect` não zera mais as assinaturas de eventos. |
| [#190](https://github.com/evolution-foundation/evolution-go/pull/190) | deps: bump whatsmeow to fix stream-error reconnect loop that stalls offline q… | LineckerN | ✅ Reimplementado | Bump do whatsmeow feito no branch `deps/whatsmeow-bump` (para a versão de 29/09, Go 1.26). |
| [#191](https://github.com/evolution-foundation/evolution-go/pull/191) | fix(whatsmeow): rotear Chat para DestinationJID em mensagens enviadas por apa… | douglasanpa | ⏸ Não aplicado | Roteia `Chat` para `DestinationJID` em mensagens enviadas por outro aparelho. Muda o payload do webhook e não consegui confirmar a premissa; avaliar com log real. |
| [#192](https://github.com/evolution-foundation/evolution-go/pull/192) | fix: prevent auto-reconnect during QR pairing phase | RoushanKhalid | ⏸ Não aplicado | Não reconectar via `ReconnectClient` durante o pareamento. Risco de ressuscitar cliente já derrubado pelo teardown do QR; avaliar ao vivo. |
| [#194](https://github.com/evolution-foundation/evolution-go/pull/194) | fix(whatsmeow): create the sqlstore container once instead of on every StartC… | EcoosUP | ⏩ Superado | Leak de pool — coberto. |
| [#195](https://github.com/evolution-foundation/evolution-go/pull/195) | fix(whatsmeow): Archive event panics before reaching the webhook | EcoosUP | ⏩ Superado | Panic Archive — coberto. |
| [#196](https://github.com/evolution-foundation/evolution-go/pull/196) | fix(concurrency): guard the three shared maps with a mutex | EcoosUP | ✅ Aplicado | Mesclado: `safemap` para `clientPointer`/`myClientPointer`/`killChannel` (fim do `concurrent map writes`). |
| [#197](https://github.com/evolution-foundation/evolution-go/pull/197) | fix(whatsmeow): back off the reconnect loop instead of spinning forever | EcoosUP | ✅ Tratado de outro jeito | Backoff da reconexão automática implementado no fork (PR #62): 0, 5, 10, 20 s... até 5 min, com jitter, reset após 1 min estável e configurável (`RECONNECT_BACKOFF_BASE_SEC`/`MAX_SEC`); pedidos pela API não são espaçados. |
| [#198](https://github.com/evolution-foundation/evolution-go/pull/198) | ci: run go build, vet and test on pull requests to develop | pastoriniMatheus | ✅ Reimplementado | CI (build/vet/test) — reescrito para `main` e com `-race`, em `.github/workflows/ci.yml`. |
| [#199](https://github.com/evolution-foundation/evolution-go/pull/199) | fix: conecta ao WhatsApp corretamente (client outdated + vazamento de conexao… | intelektos | 🟡 Parcial | Aplicado: `store.SetWAVersion` (a versão buscada/configurada nunca chegava ao handshake). Container compartilhado já coberto. Corrida do QR e bump de lib não aplicados. |
| [#200](https://github.com/evolution-foundation/evolution-go/pull/200) | fix: close leaked sqlstore container on client restart/reconnect | alexmagnoreis | ⏩ Superado | Leak de pool — coberto. |
| [#201](https://github.com/evolution-foundation/evolution-go/pull/201) | Include Windows setup instructions in README | Mustapure | ⏸ Não aplicado | README (Windows). Trivial; o bloco duplica "Setup" — melhor reescrever à mão. |
| [#202](https://github.com/evolution-foundation/evolution-go/pull/202) | Add Windows setup instructions to README | Mustapure | ⏸ Não aplicado | README (Windows). Igual ao #201. |
| [#206](https://github.com/evolution-foundation/evolution-go/pull/206) | fix(whatsmeow): reuse shared sqlstore.Container for PostgresAuthDB | meguisouza | ⏩ Superado | Leak de pool — coberto. |
| [#207](https://github.com/evolution-foundation/evolution-go/pull/207) | feat(send): upload a high-quality thumbnail for link previews | cateim | 📝 Proposta | Thumbnail HQ em `/send/link` (572 linhas; resolve #103). Ver propostas. |
