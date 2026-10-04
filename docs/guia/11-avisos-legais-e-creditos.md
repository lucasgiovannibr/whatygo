# 11. Avisos legais e créditos

> Este capítulo explica, em linguagem simples, como o WhatyGo respeita as regras do projeto original. **Não é aconselhamento jurídico.** Ele foi escrito a partir dos arquivos `LICENSE`, `NOTICE` e `TRADEMARKS.md` do Evolution Go. Em caso de dúvida séria (principalmente uso comercial), consulte um advogado ou peça confirmação por escrito à Evolution Foundation (suporte@evofoundation.com.br).

## De onde vem o WhatyGo

O WhatyGo é um **fork** (cópia modificada, publicada abertamente) do **Evolution Go**, projeto da **Evolution Foundation** (<https://github.com/evolution-foundation/evolution-go>). **Não é um produto oficial** da Evolution Foundation, que não o patrocina, endossa nem tem relação com ele. A conexão com o WhatsApp usa a biblioteca [whatsmeow](https://github.com/tulir/whatsmeow), de Tulir Asokan.

"WhatsApp" é marca da WhatsApp LLC / Meta. O WhatyGo não é afiliado a eles e usa um caminho **não oficial**; veja os riscos em [Configuração e segurança](./08-configuracao-e-seguranca.md#cuidados-para-não-ser-banido).

## O que o código permite (licença)

O código é licenciado sob **Apache 2.0**, com **duas condições adicionais** do projeto original (arquivo [`LICENSE`](../../LICENSE)). Você pode usar, modificar e redistribuir, inclusive comercialmente, **desde que**:

| Regra | O que significa na prática | Como o WhatyGo cumpre |
|---|---|---|
| Manter o `LICENSE` e o `NOTICE` | Os arquivos de licença e os créditos continuam no repositório. | Mantidos na raiz, sem alteração. |
| Indicar as mudanças | Deixar claro que o código foi modificado (Apache 2.0, §4). | `CHANGELOG.md`, `FORK-TRIAGE.md` e [O que mudou no fork](./10-o-que-mudou-no-fork.md). |
| **1(a)** Não remover o copyright do console (painel) | O painel deve continuar mostrando a autoria do original. | O painel mostra *"Baseado no Evolution Go · © 2026 Evolution Foundation · Apache 2.0"* no menu e na tela de entrada. |
| **1(b)** Avisar que o Evolution Go está sendo usado | Um aviso visível ao administrador, acessível pela documentação ou pela tela de configurações. | O aviso do painel, o README e este capítulo. |

**E a ativação de licença do original?** Ela era um recurso técnico do programa (um registro num serviço da Evolution Foundation), e **não uma condição do `LICENSE`**: nem o `LICENSE`, nem o `NOTICE`, nem o `TRADEMARKS.md` mandam usá-la, e a Apache 2.0 permite modificar o programa e trocar esse recurso. O WhatyGo a removeu, por isso **não envia nenhum dado** a esse serviço. Continuam valendo tudo o que está na tabela acima. Quem se registrou no serviço do original no passado continua sujeito aos termos que aceitou lá; isso não depende do WhatyGo. A análise completa está em [`docs/LICENCA-ANALISE.md`](../LICENCA-ANALISE.md).

## O que a marca permite (nome e visual)

A política de marcas ([`TRADEMARKS.md`](../../TRADEMARKS.md)) é separada da licença do código. Ela protege os nomes **"Evolution Foundation", "Evolution" e "Evolution Go"**, o logotipo e a identidade visual (paleta de cores, tipografia, raio de borda e a linha de copyright).

- **Pode** dizer, de forma verdadeira, que o seu software é **"baseado no Evolution Go"** ou **"fork do Evolution Go"**, sem sugerir aprovação deles (§2.1 e §2.3).
- Quem publica uma **interface modificada** deve **remover as marcas e o visual** do original e escolher um nome **claramente distinto** (§4.2).
- **Não pode** sugerir patrocínio ou afiliação (§4.3), nem modificar, recolorir ou criar derivados do logotipo do original (§4.4).

### O que o WhatyGo fez para seguir isso

| Item | Situação |
|---|---|
| Nome do produto: **WhatyGo** | ✅ No painel, na documentação, no README, no Swagger, na extensão do navegador, nos logs e na imagem Docker. |
| Logotipo | ✅ Próprio (o painel não usa nenhum arquivo nem desenho do logotipo original). |
| Cores | ✅ O painel usa um índigo próprio. A paleta listada na política (verde neon `#00ffa7` e o conjunto de cinzas) **não** é usada. |
| Raio de borda e tipografia | ✅ Valores próprios (raio de 8 px e 12 px; fonte Inter, de licença livre). |
| Textos e links de apoio do original (suporte, comunidade, termos, hospedagem parceira) | ✅ Removidos. O WhatyGo não fala em nome deles. |
| Menção ao original | ✅ Só como origem: "Baseado no Evolution Go" e "fork do Evolution Go". |
| Imagem Docker | ✅ `ghcr.io/lucasgiovannibr/whatygo` (a antiga `.../evolution-go` não é mais atualizada). |
| Nome do **repositório** no GitHub (`evolution-go`) | ⚠️ **Pendente (ação sua, no GitHub):** renomear em *Settings → Repository name*. O GitHub redireciona os endereços antigos. |
| Nomes **internos** do código (módulo Go `github.com/evolution-foundation/evolution-go`, nomes das métricas `evolution_*`, usuário `evolution` do contêiner, pasta `cmd/evolution-go`) | ➖ Mantidos de propósito: não são interface nem material de divulgação, e trocá-los quebraria painéis de monitoramento e integrações. O caminho do módulo Go também referencia a origem do código. |

### Uma contradição nos documentos do original

Os dois documentos do original não dizem exatamente a mesma coisa sobre o logotipo:

- o `LICENSE` (1(a)) diz para **não remover nem modificar o logotipo e o copyright** no console do Evolution Go;
- o `TRADEMARKS.md` (4.2) diz que quem publica uma interface **modificada** deve **remover** o logotipo e as marcas.

O WhatyGo adotou a leitura **mais cautelosa**: o painel é uma **reescrita completa** (o original nem tinha o código-fonte do painel no repositório, só um arquivo compilado), então o logotipo do original não é mantido, mas **o aviso de copyright e a menção ao Evolution Go são mantidos**, porque um aviso de copyright não é uso de marca e atende ao `LICENSE`. É uma interpretação razoável, **não uma garantia**. A forma de ter certeza é pedir à Evolution Foundation uma **confirmação por escrito** de que esse tratamento está de acordo (veja abaixo).

### Sugestão: pedir confirmação por escrito

Se o WhatyGo for usado comercialmente ou amplamente divulgado, vale enviar um e-mail curto para suporte@evofoundation.com.br, apresentando o fork (nome, endereço do repositório, o que é mantido: `LICENSE`, `NOTICE`, copyright e aviso "baseado no Evolution Go"; o que foi trocado: nome, logotipo, cores) e pedindo que confirmem que está de acordo com o `LICENSE` e o `TRADEMARKS.md`. Guarde a resposta.

## Créditos

- **Evolution Foundation** — projeto Evolution Go, o código-base deste fork.
- **Tulir Asokan** — [whatsmeow](https://github.com/tulir/whatsmeow), a biblioteca do protocolo do WhatsApp.
- **purpshell** — [meowcaller](https://github.com/purpshell/meowcaller), a biblioteca de chamadas usada de forma experimental.
- Fontes **Inter** e **JetBrains Mono** (licença OFL), usadas no painel.
- Todas as demais dependências, listadas em `go.mod` e `manager/package.json`, com as suas respectivas licenças.
