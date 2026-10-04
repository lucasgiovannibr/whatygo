# Política de segurança

## Como relatar uma vulnerabilidade

**Não abra uma *issue* pública** para falhas de segurança: qualquer pessoa leria os detalhes antes de existir uma correção.

Use o **relato privado de vulnerabilidades do GitHub**: na página do repositório, aba **Security** → **Report a vulnerability**. Só você e os mantenedores veem o relato, e a conversa e a correção podem ser combinadas ali, em privado.

Se o botão não estiver disponível, abra uma *issue* **sem nenhum detalhe técnico**, dizendo apenas que tem um relato de segurança e pedindo um canal privado; os mantenedores respondem indicando um.

### O que incluir

- O que é a falha e qual o impacto (o que um atacante consegue fazer);
- Os passos para reproduzir, com a **versão** (etiqueta da imagem ou commit) e a forma de instalação;
- Uma sugestão de correção, se tiver (opcional);
- **Nunca** inclua chaves reais, tokens, senhas, nem dados de pessoas: use valores inventados.

### O que esperar

O projeto é mantido por voluntários. A meta é **responder em até 7 dias** e dizer se o relato foi aceito, mas **não há prazo nem garantia**. Para falhas confirmadas, a correção sai primeiro na `main` e na imagem `latest`, e o `CHANGELOG.md` registra a mudança depois que ela está disponível. Quem relata é citado como autor da descoberta, se quiser. Não há programa de recompensa.

## Versões com correção

Só a **versão mais recente** (a `main` e a imagem `ghcr.io/lucasgiovannibr/whatygo:latest`) recebe correções de segurança. Versões antigas e a imagem do projeto original **não** são atendidas aqui.

## O que está no escopo

Está no escopo o que este repositório contém: o servidor, o painel (`manager/`), a extensão `passkey-helper`, os arquivos Docker e as GitHub Actions.

Fora do escopo daqui, relate a quem mantém:

| Problema em... | Relate a... |
|---|---|
| Código herdado do projeto original que o WhatyGo não alterou | A [Evolution Foundation](https://github.com/evolution-foundation/evolution-go) (e avise-nos também, para tratarmos no WhatyGo) |
| A biblioteca [whatsmeow](https://github.com/tulir/whatsmeow) | Aos mantenedores dela |
| A biblioteca de chamadas [meowcaller](https://github.com/purpshell/meowcaller) | Aos mantenedores dela |
| O próprio WhatsApp | À Meta |
| Outra dependência | Ao mantenedor da dependência |

Ataques que exigem acesso à máquina do servidor, ao arquivo `.env` ou à `GLOBAL_API_KEY` já presumem controle total e normalmente **não** contam como vulnerabilidade.

## Como o projeto se protege

O WhatyGo já faz, entre outras coisas: recusa chaves de exemplo, roda o contêiner sem privilégios de administrador, aceita URLs de mídia só para endereços públicos, mantém o MinIO privado (links temporários), não envia o token da instância nos eventos e limita o tamanho das requisições. A lista e o que cabe a **quem instala** (HTTPS, firewall, backup, guarda da chave global) estão em [Configuração e segurança](./docs/guia/08-configuracao-e-seguranca.md).

## Boas práticas para quem hospeda

1. Use uma `GLOBAL_API_KEY` longa e aleatória (`openssl rand -hex 32`) e guarde-a como senha.
2. Coloque HTTPS na frente e feche todas as portas, menos 80/443 (e SSH). Nunca exponha o banco de dados.
3. Mantenha a imagem atualizada e faça backup dos bancos.
4. Não exponha `/debug/pprof` nem `/metrics` na internet.
