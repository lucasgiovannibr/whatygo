# docker/

| Pasta ou arquivo | Para quê |
|---|---|
| [`instalacao-simples/`](./instalacao-simples) | Instalação para uso real, a mais curta (Compose com Postgres). É a do [guia de instalação](../docs/guia/02-instalacao.md). |
| [`examples/`](./examples) | Exemplos de Compose para cenários maiores: completo (RabbitMQ, NATS, MinIO) e Swarm. |
| [`test-stack/`](./test-stack/README.md) | Pilha descartável para testar uma mudança (porta 8100, banco novo). Só para desenvolvimento. |
| `entrypoint.sh` | Usado pelo [`Dockerfile`](../Dockerfile): acerta a posse dos volumes e baixa os privilégios antes de iniciar o servidor. |

A imagem oficial do projeto é `ghcr.io/lucasgiovannibr/whatygo`.
