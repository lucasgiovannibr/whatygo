# Pilha de testes isolada

Sobe o WhatyGo com um Postgres novo, numa porta própria (**8100**), sem tocar em nenhuma outra
instalação ou volume da máquina. Serve para testar uma mudança de ponta a ponta antes de mesclar.

Não é uma instalação para uso real: para isso, veja [`../instalacao-simples`](../instalacao-simples).

## Passo a passo

Na raiz do repositório:

```bash
# 1. Crie o .env (o git o ignora) e preencha as duas chaves
cp docker/test-stack/.env.example docker/test-stack/.env

# 2. Construa a imagem local do código que você quer testar
docker build -t whatygo:test .

# 3. Suba a pilha
docker compose -f docker/test-stack/docker-compose.yml up -d
```

O painel fica em <http://localhost:8100/manager>; entre com a `GLOBAL_API_KEY` do seu `.env`.
`GET http://localhost:8100/health` mostra se os dois bancos estão de pé.

Para testar outra versão do código, refaça o passo 2 e rode o passo 3 de novo (o Compose recria só o
que mudou).

## Parar e recomeçar do zero

```bash
docker compose -f docker/test-stack/docker-compose.yml down       # para, mantém os dados
docker compose -f docker/test-stack/docker-compose.yml down -v    # para e APAGA os dados
```

Os dados (banco, sessões do WhatsApp e logs) ficam em volumes do Docker chamados `evofork-test_*`.
O nome do projeto Compose não segue o nome atual de propósito: trocá-lo criaria volumes novos e
faria a pilha começar vazia. Apague os volumes (`down -v`) se quiser renomear.

## O que mais há aqui

| Arquivo | Para quê |
|---|---|
| `docker-compose.yml` | Postgres 15 + WhatyGo (`whatygo:test`) na porta 8100 |
| `call-stream-test.py` | Teste manual de chamada ao vivo (áudio e vídeo) pelo WebSocket `/call/stream`; veja o cabeçalho do arquivo e o capítulo de chamadas da wiki |
| `fixtures/test.png` | Imagem genérica (gradiente) para testar o envio de mídia |

## Cuidados

- `CONNECT_ON_STARTUP` está ligado: instâncias já pareadas neste volume reconectam ao WhatsApp
  sozinhas quando a pilha sobe.
- O webhook de uma instância de teste aponta, em geral, para `host.docker.internal`; se nada estiver
  escutando lá, o log mostra tentativas de entrega falhando. É esperado.
