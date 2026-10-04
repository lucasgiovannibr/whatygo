# 5. O painel (`/manager`)

O painel é um site que o próprio servidor entrega. Serve para operar o WhatyGo sem programar. Foi **refeito do zero** no WhatyGo (o original não tinha nem o código-fonte no repositório): é mais enxuto, tem tema claro e escuro e um menu em gaveta para telas pequenas.

Endereço: `http://SEU-SERVIDOR:4000/manager`. Para entrar, use a chave global.

## Menu lateral

| Item | Para que serve |
|---|---|
| **Visão geral** | Resumo do servidor e das instâncias. Mostra quais precisam de atenção (desconectadas) e atalhos. |
| **Instâncias** | Lista (em cartões ou tabela) de todos os números. Criar, conectar, abrir e apagar. |
| **Explorador da API** | Testa qualquer endereço da API pelo navegador: escolhe a rota, preenche e vê a resposta. Para quem programa. |
| **Swagger** | Abre a documentação interativa da API. |

No rodapé do menu há o estado do servidor (verde = tudo certo), o botão de tema claro/escuro, o recolher do menu e **Sair**. Abaixo fica o crédito de origem (veja [Avisos legais](./11-avisos-legais-e-creditos.md)).

## Dentro de uma instância

Ao abrir uma instância há cinco abas:

### Geral
O estado da conexão, as **credenciais** (o token desta instância, com botão de copiar) e para onde os eventos estão indo. No fim, a **zona de perigo** (desconectar e apagar).

### Webhook e eventos
- **Webhook**: o endereço do seu sistema que receberá um aviso (POST) a cada evento escolhido.
- **Eventos**: marque o que quer receber. São 16 tipos, em quatro grupos:
  - *Mensagens*: recebida, enviada, confirmação de leitura, clique em botão;
  - *Presença e chats*: online/visto por último, digitando, fixar/arquivar/silenciar;
  - *Sessão*: conexão, QR Code e pareamento, chamadas, histórico;
  - *Contatos e grupos*: contatos, grupos, canais, etiquetas, foto de perfil, recado.
- **Outros canais**: RabbitMQ, WebSocket e NATS, que entregam os mesmos eventos. Cada um pode ser *Padrão* (segue o servidor), *Ativo* ou *Inativo*.

Salvar aqui **não reinicia nem conecta** a instância. Vale na hora se ela estiver rodando.

### Comportamento
Interruptores simples:

| Opção | O que faz |
|---|---|
| **Sempre online** | Mantém o número como "online" mesmo sem atividade. (Atenção: com isso ligado, o celular pode deixar de tocar notificações.) |
| **Rejeitar chamadas** | Recusa ligações automaticamente e, se quiser, manda um texto de aviso a quem ligou. |
| **Chamadas** | Liga o motor de chamadas (atender e ligar pelo navegador). Vale na próxima conexão. Veja [Chamadas](./07-chamadas.md). |
| **Marcar como lidas** | Marca as mensagens recebidas como lidas sozinho. |
| **Ignorar grupos** | Não processa mensagens de grupos. |
| **Ignorar status** | Não processa os "stories". |

### Chamadas
Telefone no navegador: atender, ligar, falar pelo microfone, ver o vídeo, e o histórico de chamadas. Detalhes em [Chamadas de voz e vídeo](./07-chamadas.md).

### Testar envio
Um formulário para cada tipo de mensagem, com exemplos prontos: texto, link, mídia (imagem, vídeo, áudio, documento), enquete, figurinha, localização, contato, botões, lista, carrossel e status. Há modelos prontos de botões e carrossel para testar. Cada envio feito na sessão aparece numa lista ao lado (que some ao sair da página).

> **Atenção:** os envios de mídia e de **status** vão para contatos de verdade. O painel pede confirmação no status.

## Dicas

- **Celular:** o menu vira uma gaveta em telas pequenas.
- **Sessão expirou:** o painel volta para a tela de entrada; é só informar a chave de novo.
- **Em outro endereço:** se o painel está num endereço e a API em outro, a tela de entrada tem o botão **Alterar** ao lado do servidor.
