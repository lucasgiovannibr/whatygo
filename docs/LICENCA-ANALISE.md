# Análise do mecanismo de licença herdado do upstream

> **Estado (2026-10-03):** a Opção A (seção 9) foi escolhida e implementada; o resultado e a
> verificação estão na seção 10. As seções 2 a 7 descrevem o código **como era** (`pkg/core` foi
> removido; para ler o original: `git show fd6d77d^:pkg/core/c0.go`).

Data da análise: 2026-10-03. Escopo: somente leitura do código desta branch (`pkg/core/c0.go`,
`pkg/core/license_swagger.go`, `cmd/evolution-go/main.go`, `manager/src/...`). Nada foi executado
contra o serviço de licença do upstream (é um serviço de terceiros: não foi chamado, nem para testar).

> Referências de linha são de `pkg/core/c0.go` nesta branch. Os identificadores do arquivo são
> ofuscados; abaixo uso o papel de cada um (`_kni` = chave de licença, `_z14` = id da instalação,
> `_cdo()` = URL do servidor de licença, `_c4m` = chamada de ativação, `_814l` = heartbeat).

## 1. Resumo

1. **A licença não está entrelaçada com funcionalidade real.** `DeriveInstanceToken`,
   `ComputeSessionSeed`, `ValidateRouteAccess` e os contadores `TrackMessageSent/Recv` **não têm
   nenhum chamador** fora de `pkg/core` (busca em todo o repositório; só `cmd/evolution-go/main.go`
   importa o pacote, e só usa `SetDB`, `MigrateDB`, `InitializeRuntime`, `GateMiddleware`,
   `LicenseRoutes`, `StartHeartbeat`, `Shutdown`). O valor `_rch` que o gate põe no contexto do gin
   também não é lido por ninguém. O token de uma instância é o que o cliente envia ao criar
   (`pkg/instance/handler/instance_handler.go:72`) ou um UUID; **não** vem de `DeriveInstanceToken`.
   Logo, remover ou trocar o mecanismo **não muda tokens nem sessões** de instâncias existentes.
2. O único efeito real do mecanismo é (a) o **gate HTTP** que responde 503 `LICENSE_REQUIRED` e
   (b) a **comunicação com `https://license.evolutionfoundation.com.br`**.
3. Há **vazamentos de segurança/privacidade** no mecanismo atual (seção 7), em especial: a
   `GLOBAL_API_KEY` (chave mestra) é enviada ao servidor de terceiros e guardada em claro no banco, e
   `/license/status` (sem autenticação) revela 12 caracteres dela.
4. Nada no repositório torna a troca **ilegal** (seção 8). Há um ponto de atenção sobre o aviso de
   uso/marca que já é tratado fora desta tarefa.

## 2. Mapa do código

| Peça | Onde | Papel |
|---|---|---|
| Estado, banco, HTTP, gate, rotas, heartbeat | `pkg/core/c0.go` (978 linhas) | Tudo em um arquivo |
| Rotas no Swagger | `pkg/core/license_swagger.go` | Só anotações (as rotas reais são inline) |
| Ligação | `cmd/evolution-go/main.go:241` (`GateMiddleware`), `:244` (`LicenseRoutes`), `:489-494` (`SetDB`, `MigrateDB`, `InitializeRuntime(tier, version, cfg.GlobalApiKey)`), `:533` (`StartHeartbeat`), `:562` (`Shutdown`) | |
| Painel | `manager/src/api/session.ts`, `features/auth/login-page.tsx`, `features/auth/license-callback-page.tsx`, `lib/http.ts:150`, `stores/auth.ts` (`licenseState`) | Fluxo de registro + tratamento de `LICENSE_REQUIRED` |
| CI | `.github/workflows/ci.yml:47-50` | `gofmt` ignora `pkg/core/` de propósito |

O endereço do servidor é montado a partir de pedaços de string (`c0.go:39-43`) para não aparecer
inteiro no binário: `https://license.evolutionfoundation.com.br`. As variáveis `_6np1`/`_96`
(`c0.go:31-37`) permitiriam trocá-lo por ldflags, mas **nada as define** (Dockerfile e Makefile só
passam `main.version`). É o **único** endereço da Evolution Foundation chamado pelo código Go; o
resto das menções a eles é só caminho de import (`github.com/evolution-foundation/evolution-go/...`),
README e documentação.

## 3. Fluxo ponta a ponta

### 3.1 Na inicialização (`InitializeRuntime`, `c0.go:441-501`)

1. Carrega ou cria o **id da instalação** (`_ggnz`, `c0.go:254`): lê `instance_id` de `runtime_configs`;
   se não houver, deriva de `hostname|MAC` (`_tym7`) ou, na falta, de um UUID aleatório, e grava.
2. Três caminhos, nesta ordem:
   - **A. Há `api_key` em `runtime_configs`** (já ativado antes): considera a licença **ativa na hora, sem
     perguntar a ninguém**; calcula `sha256(chave + id)`; e dispara, em segundo plano e sem bloquear,
     `POST /v1/activate` (`_c4m`). Se a chamada falhar ou o servidor responder outra coisa, só imprime um
     aviso; **o resultado é ignorado**.
   - **B. Não há chave no banco, mas `GLOBAL_API_KEY` está definida** (sempre está: o servidor não sobe sem
     ela): **usa a `GLOBAL_API_KEY` como chave de licença** e faz `POST /v1/activate` **de forma
     bloqueante** (timeout de 10 s). Se o servidor responder `200 {"status":"active"}`, grava a
     `GLOBAL_API_KEY` em `runtime_configs.api_key` e ativa. Senão, imprime o aviso de "License
     Registration Required" e o gate fica fechado. **Isso se repete a cada inicialização enquanto não
     ativado.**
   - **C. Sem chave e sem `GLOBAL_API_KEY`** (não ocorre na prática): se `EVOLUTION_OPERATOR_EMAIL` estiver
     definida, `POST /v1/register/auto` com o e-mail; se o e-mail já estiver registrado lá, o servidor devolve
     `api_key` e o código ativa e grava.

### 3.2 Fluxo do painel (o que o dono descreveu)

1. `GET /license/status` → `{status, instance_id, api_key mascarada}`.
2. Se não estiver `active`: `GET /license/register?redirect_uri=<origem do painel>/manager/license/callback`.
   O servidor local chama `POST /v1/register/init` (`tier`, `version`, `instance_id`, `redirect_uri`) e
   devolve `register_url` (página do upstream; de lá sai o e-mail com o código).
3. O navegador volta para `/manager/license/callback?code=...` e o painel chama
   `GET /license/activate?code=...`.
4. O servidor local faz `POST /v1/register/exchange` (`authorization_code`, `instance_id`). Se a troca
   falhar por qualquer motivo (inclusive rede), **usa o próprio código como se fosse a chave** (`_58`,
   `c0.go:891`) e testa em `POST /v1/activate`. Se OK, grava `api_key`, `tier`, `customer_id` e ativa.

### 3.3 Em operação

- **Heartbeat** a cada 30 min (`hbInterval`, `c0.go:373`): `POST /v1/heartbeat` com `instance_id`,
  `uptime_seconds`, `version` e, se houver, contagens de mensagens (hoje **sempre zeradas**: nada chama
  `TrackMessageSent/Recv`). Uma falha só gera aviso.
- **Desligamento** (`Shutdown`, `c0.go:861`): `POST /v1/deactivate` (`instance_id`), timeout de 5 s.

### 3.4 O que é enviado, para onde

Todos os pedidos vão para `https://license.evolutionfoundation.com.br` (TLS padrão, sem pinagem,
cliente HTTP com timeout de 10 s, respeita `HTTP_PROXY`/`HTTPS_PROXY` do ambiente).

| Chamada | Quando | Dados enviados | Cabeçalhos de credencial |
|---|---|---|---|
| `POST /v1/activate` | caminho A (segundo plano), B (bloqueante), ao ativar | `instance_id`, `version` | `X-Api-Key: <chave>` e `X-Signature: HMAC-SHA256(corpo, <chave>)` |
| `POST /v1/register/init` | `GET /license/register` | `tier`, `version`, `instance_id`, `redirect_uri` (URL pública do painel) | nenhum |
| `POST /v1/register/exchange` | `GET /license/activate` | `authorization_code`, `instance_id` | nenhum |
| `POST /v1/register/auto` | caminho C | `email` (do operador), `tier`, `version`, `instance_id` | nenhum |
| `POST /v1/heartbeat` | a cada 30 min | `instance_id`, `uptime_seconds`, `version`, contagens | idem `/v1/activate` |
| `POST /v1/deactivate` | ao desligar | `instance_id` | idem `/v1/activate` |

Observações:

- `<chave>` é a licença emitida pelo upstream **ou a `GLOBAL_API_KEY`** (caminho B). Neste caso a chave
  mestra de administração do servidor viaja para um terceiro a cada inicialização não ativada, em todo
  heartbeat e no desligamento.
- O HMAC não protege nada: a mesma chave segue em `X-Api-Key`.
- `instance_id` é uma impressão digital da máquina (hostname + MAC). `_tym7` copia os primeiros 16 bytes
  de `hostname|MAC` quase literalmente para o "UUID" (`c0.go:281-290`): um hostname curto aparece em
  hexadecimal dentro do id.

## 4. O que o gate bloqueia

`GateMiddleware` (`c0.go:638`) é um middleware global do gin, registrado **antes** das rotas e **antes**
da autenticação. Sem licença ativa, responde `503 {"code":"LICENSE_REQUIRED", ...}` a **toda** rota HTTP,
**exceto**: `/health`, `/server/ok`, `/favicon.ico`, `/license/status|register|activate`, prefixos
`/manager`, `/assets`, `/passkey-ceremony`, `/swagger`, `/ws`, e qualquer caminho terminado em `.svg`,
`.css`, `.js`, `.png`, `.ico`, `.woff`, `.woff2`, `.ttf`.

O que o gate **não** faz:

- Não derruba nada em segundo plano. As sessões do WhatsApp, o recebimento de eventos e os webhooks/filas
  continuam funcionando sem licença; só a API HTTP responde 503.
- Não valida nada além de "o processo marcou a licença como ativa": `ValidateContext` compara
  `sha256(chave+id)` guardado em memória com o recalculado, o que é verdadeiro sempre que a ativação
  aconteceu neste processo. Não há expiração, revalidação nem revogação (uma licença revogada no servidor
  continua valendo localmente, porque o resultado do caminho A é ignorado).
- Não protege `/license/*` nem `/manager`: elas ficam abertas, sem autenticação.

## 5. Sem rede

| Situação | Resultado |
|---|---|
| Instalação **já ativada** (chave no banco), sem rede | Sobe normalmente, ativa e funciona. Avisos de "Remote activation notice failed" e "Heartbeat failed" no log; contagens ficam para a próxima tentativa. Funciona indefinidamente (sem período de carência, sem expiração). |
| Instalação **nova**, sem rede | Caminho B espera até 10 s, falha e deixa o gate fechado. Registrar pelo painel também exige rede. **Não há como ativar offline.** |
| Servidor de licença fora do ar | Idem. O upstream pode, portanto, impedir **novas** instalações (ou re-implantações com banco novo) a qualquer momento. |
| Desligamento sem rede | O `deactivate` expira em 5 s e é ignorado. |

## 6. O que fica no banco e o que depende de quê

Tabela `runtime_configs` (`id`, `key` único, `value` texto, `created_at`, `updated_at`), criada por
`AutoMigrate` na inicialização, no banco principal (`db`). Chaves:

| `key` | Conteúdo | Quem lê |
|---|---|---|
| `instance_id` | UUID da instalação | só o mecanismo de licença |
| `api_key` | **chave de licença em claro** (ou a `GLOBAL_API_KEY`, caminho B) | só o mecanismo de licença |
| `tier` | `evolution-go` | idem |
| `customer_id` | id numérico do cliente no upstream (opcional) | idem |

Dependências, no sentido "o que precisa do quê":

- Funcionalidade real → licença: **nenhuma** (seção 1, item 1).
- Painel → licença: o login chama `/license/status` e, se não estiver `active`, inicia o registro;
  `lib/http.ts:150` derruba a sessão em `LICENSE_REQUIRED`/`LICENSE_INVALID`/`LICENSE_EXPIRED`;
  `stores/auth.ts` exige `licenseState === 'licensed'` para considerar o usuário logado.
- Swagger → `license_swagger.go` (anotações que existem para o `swag init` não perder as rotas, porque
  elas são registradas inline).
- `docs/`, README e `FORK-TRIAGE.md`/`FEATURE-PROPOSALS.md` (item 42) descrevem o mecanismo.
- Nenhum outro pacote lê `runtime_configs`, e nenhuma outra tabela referencia essa.

## 7. Achados de segurança/privacidade no mecanismo atual

1. **Chave mestra enviada a terceiro** (caminho B): a `GLOBAL_API_KEY` vai no cabeçalho `X-Api-Key` a cada
   inicialização não ativada, a cada heartbeat e no desligamento.
2. **Chave mestra em claro no banco** (`runtime_configs.api_key`): quem lê o banco (backup, outro serviço
   com acesso) obtém a chave de administração.
3. **`/license/status` sem autenticação** devolve `instance_id` e `api_key` mascarada como
   `primeiros 8 + "..." + últimos 4` caracteres: 12 caracteres da chave mestra para qualquer um que
   alcance a porta.
4. **Log de inicialização** imprime os mesmos 12 caracteres (`c0.go:464`). Chaves com menos de 8
   caracteres derrubam o processo (fatiamento fora do limite) no log e em `/license/status`.
5. **`/license/register` e `/license/activate` sem autenticação**: em um servidor ainda não ativado, qualquer
   cliente de rede pode iniciar o registro ou tentar ativar com um código.
6. **Impressão digital da máquina** (hostname/MAC) enviada ao terceiro, e `redirect_uri` revela a URL
   pública do painel.
7. **Telemetria** de uso (uptime, versão, contagem de mensagens, hoje zerada) a cada 30 min.
8. **Dependência de disponibilidade** do upstream para novas instalações (seção 5).

## 8. Aspectos legais

- `LICENSE`: Apache 2.0 **mais duas condições adicionais**, ambas sobre o **frontend**: 1(a) não remover
  nem modificar o LOGO e as informações de copyright no console; 1(b) exibir aviso claro de que o Evolution
  Go é usado, visível aos administradores e acessível pela documentação ou página de configurações. Nada
  no `LICENSE`, no `NOTICE` ou no `TRADEMARKS.md` **obriga** a usar o mecanismo de ativação nem proíbe
  substituí-lo. A concessão de obras derivadas da Apache 2.0 (§2) cobre a mudança.
- **Apache 2.0 §4** continua valendo: manter `LICENSE` e `NOTICE`; manter os avisos de copyright, patente,
  marca e atribuição nos arquivos-fonte que forem mantidos; e **arquivos modificados devem ter aviso
  visível de que foram alterados** (§4(b)). Os arquivos `.go` do upstream **não têm cabeçalho de
  copyright nem SPDX** (busca em todo o repositório): não há cabeçalho próprio diferente da Apache 2.0
  a preservar nem a violar. O fork cumpre o §4(b) como já fazia: o `CHANGELOG.md`, o `FORK-TRIAGE.md`
  e o histórico do git registram as mudanças (nenhum arquivo do upstream recebeu cabeçalho). Se quiser
  uma declaração mais explícita, o §4(d) permite acrescentar linhas de atribuição próprias ao `NOTICE`;
  isso não foi feito sem sua decisão.
- `TRADEMARKS.md`: protege "Evolution Foundation", "Evolution", "Evolution Go", o logotipo, a paleta e a
  linha "© 2026 Evolution Foundation". Por isso a nova ativação **não** pode usar essas marcas no nome do
  fluxo, em e-mails, textos ou URLs, nem sugerir endosso. Referência nominativa ("baseado no Evolution Go")
  é permitida (§2.1/§2.3) e é o que o aviso de uso (1(b)) exige.
- Há uma tensão **fora do escopo desta tarefa** entre `LICENSE` 1(a) (não remover o logotipo/copyright) e
  `TRADEMARKS.md` §4.2 (UI modificada deve remover as marcas). O capítulo `11-avisos-legais-e-creditos.md`
  (branch de rebranding) já adota a leitura cautelosa e recomenda pedir confirmação por escrito à Evolution
  Foundation. Esta troca de licença não piora nem resolve isso.
- **Não verificável por aqui:** os termos de uso da página de registro do upstream (não estão no
  repositório). Eles obrigam quem se registrou lá, não o código-fonte sob Apache 2.0. A condição "uma
  licença comercial deve ser obtida do produtor" no `LICENSE` está atrelada a 1(a)/1(b), não à ativação.
- **Chaves e ativações de terceiros:** o `api_key` que existir em `runtime_configs` foi emitido pelo
  upstream. Depois da troca ele **não pode ser usado** como credencial nem enviado a lugar nenhum.
  A migração só pode, no máximo, tratar a existência da linha como marca de "instalação já em uso".
- Conclusão: **nada impede a troca**. Nenhuma cláusula, cabeçalho ou dependência exige parar.

## 9. Opções de substituição

Em todas: nenhuma chamada ao servidor do upstream; `runtime_configs` **não é apagada nem alterada** (um
binário antigo ainda a lê, o que permite voltar atrás); tokens e sessões de instâncias não mudam
(seção 1, item 1); `LICENSE`, `NOTICE` e o rodapé/aviso de uso permanecem.

### Opção A: remover o gate de vez (recomendada)

O servidor nunca responde `LICENSE_REQUIRED`. Sai o pacote de licença (HTTP, heartbeat, deactivate,
gate). Fica só um pacote mínimo, se necessário, para a compatibilidade abaixo.

- **Prós:** menor código e superfície de ataque; zero dado enviado a terceiros; funciona offline e em
  instalação nova; corrige todos os achados da seção 7 por eliminação; honesto com o que o gate realmente
  fazia (nada além de bloquear HTTP).
- **Contras:** não há como condicionar o uso a um cadastro, termos ou licença comercial. Sendo Apache 2.0,
  qualquer mecanismo no código também pode ser removido por quem receber o código, então o ganho de
  "controle" das opções B e C é limitado.
- **Riscos:** clientes antigos do painel (abas abertas, cache) ou integrações externas que chamam
  `/license/status`. Mitigação: manter `/license/status` respondendo `{"status":"active"}` (sem chave, sem
  `instance_id`) e `/license/register|activate` respondendo `410 Gone` com mensagem clara, marcados como
  *deprecated* no Swagger. O painel passa a ignorar licença.
- **Migração:** nenhuma ação do usuário. Linhas antigas em `runtime_configs` ficam inertes.

### Opção B: ativação local própria, sem terceiros

O gate continua, mas a "ativação" é um registro local: no primeiro acesso ao painel o administrador
confirma com a `GLOBAL_API_KEY` e aceita um aviso (versão do aviso, data). O servidor guarda isso numa
chave nova de `runtime_configs` (por exemplo `activation`, sem segredo) e libera o gate. Variável opcional
(por exemplo `WHATYGO_AUTO_ACTIVATE=true`) para implantação automatizada.

- **Prós:** mantém o formato "primeiro uso" e um lugar para mostrar aviso legal; nada sai da máquina.
- **Contras:** não protege nada (quem tem a chave mestra clica); cria um estado de falha novo (503 em toda
  implantação nova até alguém confirmar; quebra automações sem a variável); mais código e testes para um
  ganho pequeno.
- **Riscos:** automações e CI que sobem o servidor e chamam a API direto passam a receber 503.
- **Migração:** instalações com `api_key` já em `runtime_configs` são marcadas como ativadas
  automaticamente (só pela existência da linha; o valor nunca é lido nem enviado).

### Opção C: chave de instalação emitida pelo próprio fork, verificada offline

O dono gera um par de chaves Ed25519; a chave pública vai embutida no binário; ele assina, offline,
tokens `{cliente, validade, plano, [id da instalação]}` e entrega ao cliente, que cola no painel. O
servidor verifica a assinatura localmente. Nada de heartbeat.

- **Prós:** controle real (validade, plano, revogação por expiração), funciona offline, sem serviço de
  terceiros nem servidor próprio.
- **Contras:** exige guardar a chave privada com segurança (perdida = ninguém mais ativa; vazada = qualquer
  um emite); processo manual de emissão e suporte; como o código é aberto, a verificação pode ser removida
  por quem compilar sua própria versão (vale como contrato, não como proteção técnica); mais código e
  mais testes.
- **Riscos:** instalações existentes ficam sem licença válida do fork; é preciso um modo "legado" ou
  emitir chaves antes de publicar a versão.
- **Migração:** quem tem `api_key` antiga entra em modo legado (liberado, com aviso no painel pedindo a
  chave nova até uma data definida pelo dono). A chave antiga do upstream não é validada nem reutilizada.

### Comparação rápida

| | A: sem gate | B: ativação local | C: chave assinada |
|---|---|---|---|
| Dados a terceiros | nenhum | nenhum | nenhum |
| Funciona offline | sim | sim | sim |
| Controle de uso | não | não (cosmético) | sim (contratual) |
| Código novo | quase nenhum | pequeno | médio |
| Risco para instalações existentes | mínimo | baixo | médio |
| Recomendação | **sim** | só se quiser aviso/aceite obrigatório | só se houver plano comercial |

## 10. Implementação (Opção A) e verificação

Escolha do dono: **A, removendo também as rotas `/license/*`** (respondem 404).

O que mudou:

- Removidos `pkg/core/` inteiro e todas as ligações em `cmd/evolution-go/main.go` (gate, rotas,
  inicialização, heartbeat, desligamento). `setupRouter` perdeu o parâmetro `runtimeCtx`.
- `runtime_configs` não é mais criada nem lida. Uma tabela existente **não é tocada**: a imagem
  anterior ainda funciona sobre ela. Ela guarda a chave de licença do upstream em claro; apagar é
  decisão do operador (`DROP TABLE runtime_configs;`, documentado no guia e no CHANGELOG).
- `EVOLUTION_OPERATOR_EMAIL` saiu do `.env.example` e é ignorada.
- Painel: sem verificação de licença no login, sem a página `/manager/license/callback` (cai no
  redirecionamento padrão para `/manager`), sem `licenseState` no estado da sessão, sem tratamento
  de `LICENSE_*` em `lib/http.ts`.
- Swagger regenerado (swag v1.16.3): só desapareceram as 3 rotas de licença.
- CI/lint: `gofmt` e `golangci-lint` deixaram de excluir `pkg/core`.
- Teste-guarda `cmd/evolution-go/licensing_removed_test.go`: falha se voltar código-fonte Go que cite o
  host de licença do upstream, `LICENSE_REQUIRED`, `/v1/heartbeat`, `/v1/activate`, `/license/` ou
  `runtime_configs`, ou se `pkg/core` reaparecer.

Verificado (Docker, rede interna **sem saída**, para nada alcançar o serviço do upstream):

| Cenário | Imagem anterior | Imagem nova |
|---|---|---|
| Instalação nova, sem rede | 503 (`License Registration Required`), tentativa de contatar o host de licença | 200 em `/instance/all`, sobe em ~20 s, sem nenhuma linha de log de licença, `runtime_configs` não criada |
| Banco com `runtime_configs` preenchida (chave fabricada de teste) e uma instância com token fixo | ativa localmente; tenta chamar o host de licença (falha, sem rede) | `GET /instance/all` **idêntico byte a byte**; o token da instância continua autenticando; `runtime_configs` e `instances` **sem nenhuma alteração** (mesmos hashes e `updated_at`); `/license/*` → 404; painel servido; chave errada → 401 |

`go vet ./...` e `go test -race ./...` passam (Go 1.26 no Docker); `npm run build` (inclui `tsc`) e
`npm test` passam.
