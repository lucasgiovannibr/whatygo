<h1 align="center">WhatyGo</h1>

<p align="center">
  API de WhatsApp em Go, com painel web, eventos em tempo real e chamadas de voz e vídeo.<br />
  <strong>Fork do <a href="https://github.com/evolution-foundation/evolution-go">Evolution Go</a></strong> — não é um produto oficial da Evolution Foundation.
</p>

<p align="center">
  <a href="https://github.com/lucasgiovannibr/whatygo/actions/workflows/ci.yml"><img src="https://github.com/lucasgiovannibr/whatygo/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://opensource.org/licenses/Apache-2.0"><img src="https://img.shields.io/badge/Licen%C3%A7a-Apache%202.0-blue.svg" alt="Licença: Apache 2.0" /></a>
  <a href="https://github.com/lucasgiovannibr/whatygo/pkgs/container/whatygo"><img src="https://img.shields.io/badge/Docker-ghcr.io-blue" alt="Imagem Docker" /></a>
</p>

<p align="center">
  <a href="./docs/guia/README.md">Guia completo</a> &middot;
  <a href="./docs/guia/02-instalacao.md">Instalação</a> &middot;
  <a href="./docs/guia/04-funcionalidades.md">Funcionalidades</a> &middot;
  <a href="./CHANGELOG.md">Novidades</a>
</p>

---

## O que é

O WhatyGo é um servidor que **conecta números de WhatsApp a outros sistemas**. Você liga um número lendo um QR Code (como no WhatsApp Web) e passa a:

- **enviar mensagens** pela API: texto, mídia, enquetes, botões, carrossel, localização, contatos e mais;
- **receber mensagens e eventos** em tempo real (webhook, WebSocket, RabbitMQ ou NATS);
- **operar tudo por um painel web**, sem programar: conectar números, configurar, testar envios e atender ligações;
- **atender e fazer chamadas** de voz e vídeo 🧪 (experimental).

> **Primeira vez aqui?** Comece pelo [Guia do WhatyGo](./docs/guia/README.md): escrito para quem não é programador, com instalação passo a passo.

## Instalação em 3 comandos

Precisa do [Docker](https://www.docker.com/products/docker-desktop/) instalado.

```bash
git clone https://github.com/lucasgiovannibr/whatygo.git
cd whatygo/docker/instalacao-simples
cp .env.example .env     # preencha POSTGRES_PASSWORD e GLOBAL_API_KEY (veja o guia)
docker compose up -d
```

Depois abra **http://localhost:4000/manager**, entre com a sua `GLOBAL_API_KEY`. **Não há registro nem ativação de licença**: o servidor funciona assim que sobe. Passo a passo, com explicações e solução de problemas, em [Instalação](./docs/guia/02-instalacao.md).

A imagem pronta é pública: `ghcr.io/lucasgiovannibr/whatygo`.

## Em que este fork é diferente do original

O fork nasceu para **corrigir, melhorar e ajustar** o Evolution Go v0.7.2. Em resumo:

- **Estabilidade:** corrigidas as quedas do servidor (`concurrent map writes`, escrita concorrente em WebSocket), o vazamento de conexões do banco e a reconexão das instâncias.
- **Painel refeito do zero** (`/manager`, com código-fonte em [`manager/`](./manager/README.md)): tema claro e escuro, 16 tipos de evento, teste de 12 tipos de envio, telefone no navegador.
- **Botões e carrossel** funcionando (verificados em iPhone e WhatsApp Web); lista reenviada como botões de resposta, porque o WhatsApp não aceita lista de aparelho vinculado.
- **Sem ativação de licença:** o mecanismo herdado do original (registro num serviço de terceiros, com a chave global enviada para fora) foi removido; nada é enviado a nenhum serviço de licenciamento.
- **Chamadas** de voz e vídeo: atender, ligar e levar o áudio e o vídeo a outro sistema 🧪.
- **Segurança:** recusa chave de exemplo, contêiner sem administrador, mídia por links temporários, token fora dos eventos, links de mídia só para endereços públicos.
- **Operação:** `/health`, diagnóstico por instância, `/metrics` (Prometheus), erros com status e `code` estáveis, fila de webhook com tentativas.
- **Biblioteca do WhatsApp atualizada** (setembro/2026) e Go 1.26.

A lista completa, com a situação de cada item (✅ testado · 🟡 parcial · 🧪 experimental), está em [Funcionalidades](./docs/guia/04-funcionalidades.md) e [O que mudou no fork](./docs/guia/10-o-que-mudou-no-fork.md).

> **Vem do original?** Leia as [mudanças que podem quebrar integrações](./docs/guia/10-o-que-mudou-no-fork.md#mudanças-que-podem-quebrar-uma-integração-existente) e **faça backup do banco** antes de trocar de imagem: as migrações só andam para frente.

## Documentação

| Para... | Leia |
|---|---|
| Entender e usar (leigos e iniciantes) | [Guia do WhatyGo](./docs/guia/README.md) |
| Integrar por código | [Documentação técnica (wiki)](./docs/wiki/README.md) e o Swagger em `/swagger/index.html` |
| Ver o que mudou | [CHANGELOG.md](./CHANGELOG.md) |
| Entender as decisões do fork | [FORK-TRIAGE.md](./FORK-TRIAGE.md) e [FEATURE-PROPOSALS.md](./FEATURE-PROPOSALS.md) |
| Saber o que o WhatsApp permite e o que não permite | [WHATSMEOW-CAPABILITIES.md](./docs/WHATSMEOW-CAPABILITIES.md) |
| Comandos de desenvolvimento | [COMMANDS.md](./COMMANDS.md) (`make help`) |

## Desenvolvimento

Para compilar e rodar do código-fonte (Go 1.26+ e PostgreSQL), veja o [guia de desenvolvimento](./docs/wiki/desenvolvimento/development-guide.md) e o [COMMANDS.md](./COMMANDS.md). O painel (React + TypeScript + Vite) tem as instruções em [`manager/README.md`](./manager/README.md); o `manager/dist` é versionado e precisa ser regenerado a cada mudança no painel.

| Componente | Tecnologia |
|---|---|
| Linguagem | Go |
| HTTP | Gin |
| WhatsApp | [whatsmeow](https://github.com/tulir/whatsmeow) |
| Chamadas | [meowcaller](https://github.com/purpshell/meowcaller) (experimental) |
| Banco | PostgreSQL (GORM) |
| Filas | RabbitMQ, NATS |
| Mídia | MinIO/S3 |
| Painel | React 19, TypeScript, Vite, Tailwind |
| Documentação da API | Swagger/OpenAPI |

## Contribuindo

Correções e melhorias são bem-vindas, por *issue* ou *pull request* neste repositório. Veja o [CONTRIBUTING.md](./CONTRIBUTING.md). Em *issues*, **apague chaves, tokens e números de telefone dos logs**. Falhas de segurança: veja o [SECURITY.md](./SECURITY.md) e **não** abra *issue* pública.

## Licença, marca e créditos

- O código é licenciado sob a **Apache License 2.0, com as condições adicionais do projeto original** (manter os avisos de copyright no painel e avisar que o Evolution Go é usado). Veja [LICENSE](./LICENSE).
- **Este projeto é um fork do [Evolution Go](https://github.com/evolution-foundation/evolution-go)**, © 2026 Evolution Foundation, e não é afiliado nem endossado por ela. "Evolution Foundation", "Evolution" e "Evolution Go" são marcas da Evolution Foundation ([TRADEMARKS.md](./TRADEMARKS.md)); aqui aparecem apenas para indicar a origem.
- Créditos de terceiros (incluindo o whatsmeow, de Tulir Asokan) em [NOTICE](./NOTICE).
- Como o fork cumpre cada regra, e o que ainda está pendente: [Avisos legais e créditos](./docs/guia/11-avisos-legais-e-creditos.md).
- "WhatsApp" é marca da WhatsApp LLC. Este projeto usa um caminho **não oficial** e **não** é afiliado a ela; contas que enviam mensagens em massa ou indesejadas podem ser banidas.

## Telemetria

O servidor herdado do projeto original informa coletar dados anônimos de uso (rotas usadas e versão da API), sem dados pessoais ou sensíveis. Esse trecho faz parte do código do original e o fork não o altera.

---

<p align="center">
  Baseado no <a href="https://github.com/evolution-foundation/evolution-go">Evolution Go</a> · © 2026 Evolution Foundation · Apache 2.0
</p>
