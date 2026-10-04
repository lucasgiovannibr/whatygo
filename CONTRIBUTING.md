# Como contribuir com o WhatyGo

Obrigado por querer ajudar! O WhatyGo é baseado no [Evolution Go](https://github.com/evolution-foundation/evolution-go) que existe para **corrigir, melhorar e ajustar** o projeto. Este guia diz como relatar problemas e enviar mudanças.

Se você só quer **usar** o WhatyGo, veja o [Guia do WhatyGo](./docs/guia/README.md) e os [Problemas comuns](./docs/guia/09-problemas-comuns.md) antes de abrir uma *issue*.

## Em que ajudar

| Você quer... | Faça |
|---|---|
| Relatar um **bug** | Abra uma *issue* com o modelo "Relatório de bug". |
| Sugerir algo **novo** | Abra uma *issue* com o modelo de solicitação de recurso. **Funcionalidade nova passa por avaliação antes de virar código**: as ideias ficam em [FEATURE-PROPOSALS.md](./FEATURE-PROPOSALS.md), com esforço, risco e recomendação. |
| **Corrigir** um bug ou melhorar o que existe | Envie um *pull request* (veja abaixo). Para algo grande, abra uma *issue* antes, para alinharmos o caminho. |
| Melhorar a **documentação** | Envie um *pull request*. O guia para iniciantes fica em `docs/guia/`; a documentação técnica em `docs/wiki/`. |
| Falar de **segurança** | **Não** abra *issue* pública. Veja o [SECURITY.md](./SECURITY.md). |

## Antes de abrir uma issue

1. Procure se o problema já foi relatado.
2. Diga o que você fez, o que esperava e o que aconteceu, a **versão** (etiqueta da imagem ou commit) e como instalou.
3. Anexe as últimas linhas do log (`docker compose logs whatygo`).
4. **Apague chaves de API, tokens, senhas e números de telefone** de tudo que colar. Se não tiver certeza, troque por `XXXX`.

## Preparar o ambiente

Você precisa de Git, Docker e, para o painel, Node.js 22 ou mais novo. **Go não precisa estar instalado**: o código Go é compilado e testado dentro do Docker.

```bash
git clone https://github.com/lucasgiovannibr/whatygo.git
cd whatygo
git checkout -b minha-correcao
```

Os comandos do dia a dia estão em [COMMANDS.md](./COMMANDS.md) (`make help` lista todos). Uma pilha de testes isolada, com banco novo, está em [`docker/test-stack/`](./docker/test-stack/README.md).

### Servidor (Go)

Com Go instalado: `make test-race`, `make vet`, `make lint`. Sem Go, rode no Docker, por exemplo:

```bash
docker run --rm -v "$PWD":/src -w /src -e CGO_CFLAGS="-O1 -g0" golang:1.26 go test -race ./...
```

(O `CGO_CFLAGS` evita uma falha do compilador C na biblioteca de imagens WebP.)

### Painel (`manager/`)

```bash
cd manager
npm ci
npm run dev        # http://localhost:5173/manager
npm test
npm run build      # confere tipos e gera manager/dist
```

**O `manager/dist` é versionado** e a imagem Docker o copia: depois de mudar o painel, rode `npm run build` e inclua o `dist` no commit.

## O que o CI confere

Todo *pull request* roda estas verificações; rode-as antes de enviar:

- `gofmt`, `go build ./...`, `go vet ./...`, `go test -race ./...`, `govulncheck ./...` e o `golangci-lint`;
- no painel: `npm run typecheck`, `npm test`, `npm run build` e `npm audit` (dependências de produção);
- `docker build` e uma varredura de vulnerabilidades da imagem (Trivy).

## Enviando um pull request

1. Faça a mudança numa **branch** própria, a partir da `main` deste repositório.
2. **Um assunto por PR**: pequeno e fácil de revisar é melhor que grande.
3. **Inclua testes** para o que corrigiu ou criou. Em correção de bug, de preferência um teste que falhava antes.
4. Se mudar uma rota ou o formato de uma resposta, **regenere o Swagger**:

   ```bash
   swag init -g cmd/whatygo/main.go -d ./ --parseDependency --parseInternal -o ./docs
   ```

   (`swag` v1.16.3; pode ser rodado no Docker.)
5. Se mudar o que o usuário vê ou faz, **atualize a documentação** (`docs/guia/`, `docs/wiki/` e, para mudanças relevantes, o `CHANGELOG.md`).
6. Mensagens de commit curtas e no imperativo, com o tipo na frente: `fix(manager): ...`, `feat(calls): ...`, `docs: ...`, `test: ...`.
7. Preencha o modelo do *pull request*. Descreva **o motivo** da mudança, não só o que mudou, e diga como você testou, de preferência com um número real quando mexer no envio ou na conexão.

Os PRs são enviados **para este repositório**. Não abra PRs no repositório do projeto original a partir dele sem combinar antes.

## Regras que valem para todo mundo

- **Análise antes da correção.** Procure a causa raiz e confira a hipótese (log, teste, reprodução); evite remendos que escondem o problema.
- **Não quebre integrações sem avisar.** Mudança que altera o comportamento de uma rota, de uma variável de ambiente ou do banco precisa estar descrita no `CHANGELOG.md` com uma nota de migração. As migrações do banco só andam para frente.
- **Segredos nunca entram no repositório**: nem chaves, nem tokens, nem senhas, nem `.env`, nem números reais de clientes. Teste com valores inventados.
- **Licença e marca do projeto original.** O código é Apache 2.0 com condições adicionais ([LICENSE](./LICENSE)). Não remova o `LICENSE`, o `NOTICE`, os avisos de copyright nem o crédito "Baseado no Evolution Go" do painel. E **não use** o nome, o logotipo, a paleta ou outros elementos de marca do Evolution Go como identidade do WhatyGo ([TRADEMARKS.md](./TRADEMARKS.md)); eles só podem aparecer para indicar a origem. Detalhes em [Avisos legais e créditos](./docs/guia/11-avisos-legais-e-creditos.md).
- **Respeito.** Seja cordial e objetivo; discuta ideias, não pessoas.

## Dúvidas

Se não achou resposta no guia, abra uma *issue* explicando o que tentou. Não há prazo garantido de resposta: o projeto é mantido por voluntários.

Ao enviar uma contribuição, você concorda que ela seja licenciada sob os mesmos termos do projeto (Apache 2.0 com as condições adicionais do `LICENSE`).
