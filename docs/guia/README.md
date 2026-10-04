# Guia do WhatyGo

Guia em linguagem simples para quem **não é programador** (ou está começando) e quer entender, instalar e usar o WhatyGo.

> **WhatyGo é baseado no [Evolution Go](https://github.com/evolution-foundation/evolution-go)** (uma versão modificada dele), projeto da Evolution Foundation. Este guia não é um material oficial da Evolution Foundation, e o WhatyGo não é afiliado nem endossado por ela. Detalhes em [Avisos legais e créditos](./11-avisos-legais-e-creditos.md).

## Por onde começar

| Eu quero... | Leia |
|---|---|
| Entender o que é e para que serve | [1. Visão geral](./01-visao-geral.md) |
| Colocar para funcionar no meu computador ou servidor | [2. Instalação](./02-instalacao.md) |
| Conectar meu WhatsApp e mandar a primeira mensagem | [3. Primeiros passos](./03-primeiros-passos.md) |
| Ver **tudo** que o projeto faz, organizado por assunto | [4. Funcionalidades](./04-funcionalidades.md) |
| Aprender a usar o painel (`/manager`) | [5. O painel](./05-painel-manager.md) |
| Receber as mensagens num sistema meu (webhook e outros) | [6. Receber mensagens e eventos](./06-receber-mensagens-e-eventos.md) |
| Atender e fazer ligações pelo navegador | [7. Chamadas de voz e vídeo](./07-chamadas.md) |
| Configurar, proteger e manter no ar | [8. Configuração e segurança](./08-configuracao-e-seguranca.md) |
| Resolver um problema | [9. Problemas comuns](./09-problemas-comuns.md) |
| Saber o que o WhatyGo mudou em relação ao original | [10. O que mudou no fork](./10-o-que-mudou-no-fork.md) |

## Como ler as marcas de maturidade

Nem tudo no projeto tem o mesmo nível de confiança. Nas tabelas deste guia:

| Marca | Significa |
|---|---|
| ✅ **Testado** | Foi usado de verdade, com um número de WhatsApp real, e funcionou. |
| 🟡 **Parcial** | Funciona nos testes automáticos ou em parte do uso real; algum detalhe não foi visto num aparelho. |
| 🧪 **Experimental** | Funciona, mas depende de uma biblioteca em desenvolvimento ou foi pouco testado. Use com cuidado. |
| ⬜ **Herdado** | Já existia no projeto original e o WhatyGo não o testou de novo. |

## Precisa de mais detalhe técnico?

Esta pasta é a "porta de entrada". Para quem vai programar uma integração, a documentação técnica completa (todos os endpoints, formatos de resposta, códigos de erro, todas as variáveis) está em [`docs/wiki`](../wiki/README.md), e a referência interativa de todos os endpoints fica em `/swagger/index.html` no seu servidor.
