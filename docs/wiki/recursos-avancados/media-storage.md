# Armazenamento de Mídia

Sistema de armazenamento de arquivos de mídia do WhatyGo. Suporta MinIO, Amazon S3 e outros serviços compatíveis com S3.

## 📋 Índice

- [Visão Geral](#visão-geral)
- [Configuração](#configuração)
- [Estrutura de Arquivos](#estrutura-de-arquivos)
- [URLs Presignadas](#urls-presignadas)
- [Provedores Compatíveis](#provedores-compatíveis)
- [Exemplos Práticos](#exemplos-práticos)
- [Boas Práticas](#boas-práticas)

---

## Visão Geral

O WhatyGo armazena arquivos de mídia (imagens, vídeos, áudios, documentos) em **object storage** compatível com S3. Isso inclui serviços como MinIO, Amazon S3, Backblaze B2, DigitalOcean Spaces e outros.

### Por que Object Storage?

**Vantagens**:
- ✅ Escalável: suporta terabytes ou petabytes de dados
- ✅ Distribuído: alta disponibilidade e redundância
- ✅ Custo-efetivo: preços competitivos comparados a armazenamento tradicional
- ✅ Integração com CDN: acesso rápido globalmente
- ✅ Políticas de ciclo de vida: limpeza automática de arquivos antigos

**Comparado com armazenamento local (disco)**:
- ❌ Armazenamento local não escala horizontalmente
- ❌ Risco de perda de dados se o servidor falhar
- ❌ Complica deploys em clusters
- ❌ Sem redundância geográfica

### Arquitetura

```
┌─────────────┐
│  WhatsApp   │ Envia mídia
└──────┬──────┘
       │
       ▼
┌──────────────┐
│ WhatyGo │ Recebe arquivo
└──────┬───────┘
       │
       ▼
┌──────────────────────────────────────┐
│      MediaStorage Interface           │
└───────────────┬──────────────────────┘
                │
                ▼
┌─────────────────────────────────────┐
│       MinIO Storage Impl             │
│  (S3-Compatible Object Storage)      │
└───────────────┬─────────────────────┘
                │
                ▼
    ┌───────────────────────┐
    │   MinIO Server        │
    │   (ou S3, B2, etc)    │
    └───────────────────────┘
                │
                ▼
        ┌──────────────┐
        │  Bucket      │
        │  whatygo-    │
        │  go-medias/  │
        └──────────────┘
```

---

## Configuração

### Variáveis de Ambiente

```env
# MinIO/S3 Endpoint
MINIO_ENDPOINT=s3.amazonaws.com

# Credenciais
MINIO_ACCESS_KEY=sua-access-key
MINIO_SECRET_KEY=sua-secret-key

# Bucket
MINIO_BUCKET=whatygo-media

# Região (para AWS S3)
MINIO_REGION=us-east-1

# Usar SSL (true/false)
MINIO_USE_SSL=true

# Validade das URLs presignadas em horas (padrão e máximo: 168)
# MINIO_URL_TTL_HOURS=168

# Tornar TODOS os objetos do bucket públicos (padrão: desligado)
# MINIO_PUBLIC_BUCKET=true
```

O bucket é criado se não existir. A política do bucket **não é alterada** (antes ela era substituída por uma política pública a cada partida, o que expunha tudo o que estivesse no bucket, inclusive a mídia de outros clientes); só com `MINIO_PUBLIC_BUCKET=true` o WhatyGo aplica a política pública, e ela **substitui** a que o bucket já tinha. Em provedores sem suporte a política (Backblaze B2) as URLs presignadas continuam funcionando.

### Exemplo: MinIO Local (Docker)

```bash
# Rodar MinIO server
docker run -d   --name minio   -p 9000:9000   -p 9001:9001   -e MINIO_ROOT_USER=admin   -e MINIO_ROOT_PASSWORD=password   -v minio_data:/data   minio/minio server /data --console-address ":9001"

# Criar bucket via mc (MinIO Client)
docker run --rm   --network host   minio/mc alias set local http://localhost:9000 admin password

docker run --rm   --network host   minio/mc mb local/whatygo-media

# .env
MINIO_ENDPOINT=localhost:9000
MINIO_ACCESS_KEY=admin
MINIO_SECRET_KEY=password
MINIO_BUCKET=whatygo-media
MINIO_REGION=us-east-1
MINIO_USE_SSL=false
```

**Acesso Web Console**: http://localhost:9001

### Exemplo: AWS S3

```env
MINIO_ENDPOINT=s3.amazonaws.com
MINIO_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE
MINIO_SECRET_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
MINIO_BUCKET=meu-bucket-whatygo
MINIO_REGION=us-east-1
MINIO_USE_SSL=true
```

### Exemplo: Backblaze B2

```env
MINIO_ENDPOINT=s3.us-west-004.backblazeb2.com
MINIO_ACCESS_KEY=sua-key-id
MINIO_SECRET_KEY=sua-application-key
MINIO_BUCKET=meu-bucket
MINIO_REGION=us-west-004
MINIO_USE_SSL=true
```

### Exemplo: DigitalOcean Spaces

```env
MINIO_ENDPOINT=nyc3.digitaloceanspaces.com
MINIO_ACCESS_KEY=sua-spaces-key
MINIO_SECRET_KEY=sua-spaces-secret
MINIO_BUCKET=meu-space
MINIO_REGION=nyc3
MINIO_USE_SSL=true
```

---

## Estrutura de Arquivos

### Organização

Cada instância tem a sua pasta. O id de uma mensagem do WhatsApp não é único entre contas (duas instâncias no mesmo grupo recebem a mesma mensagem), então um caminho só com o id deixava uma instância sobrescrever o arquivo da outra, e não havia como remover os arquivos de uma instância só.

```
bucket-name/
└── whatygo-medias/
    ├── <instanceId-A>/
    │   ├── 3EB0A1B2C3.jpg
    │   └── 3EB0D4E5F6.mp4
    └── <instanceId-B>/
        └── 3EB0A1B2C3.jpg
```

### Caminho dos Arquivos

`whatygo-medias/<instanceId>/<messageId><extensão>`

**Exemplos**:
- Instância `4f3c...`, mensagem `3EB0A1B2C3`, imagem → `whatygo-medias/4f3c.../3EB0A1B2C3.jpg`

### Remoção

Ao apagar uma instância (`DELETE /instance/delete/...`) todos os arquivos da pasta dela são removidos do bucket. Arquivos gravados por versões anteriores (diretamente em `whatygo-medias/`) não pertencem a nenhuma instância e ficam onde estão.

---

## URLs Presignadas

### O que são?

**URLs presignadas** são URLs temporárias que permitem acesso a arquivos privados sem expor suas credenciais de acesso.

**Características**:
- ✅ Válidas por tempo limitado (padrão: 7 dias)
- ✅ Não requerem autenticação adicional
- ✅ Assinadas criptograficamente para segurança
- ✅ Podem ser compartilhadas publicamente durante o período de validade

### Como Funcionam

Quando você armazena ou solicita acesso a um arquivo, o WhatyGo gera automaticamente uma URL presignada com a validade configurada (`MINIO_URL_TTL_HOURS`, padrão 7 dias).

**Exemplo de URL presignada**:
```
https://s3.amazonaws.com/whatygo-media/whatygo-medias/<instanceId>/photo-123.jpg?
X-Amz-Algorithm=AWS4-HMAC-SHA256&
X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20250111%2Fus-east-1%2Fs3%2Faws4_request&
X-Amz-Date=20250111T100000Z&
X-Amz-Expires=604800&
X-Amz-SignedHeaders=host&
X-Amz-Signature=abc123def456...
```

### Validade

**Tempo de expiração**: `MINIO_URL_TTL_HOURS` (padrão e máximo: 7 dias = 168 horas)

**Após expiração**:
- A URL retorna erro `403 Forbidden`
- É necessário gerar uma nova URL através da API

---

## Provedores Compatíveis

### Amazon S3

**Configuração**:
```env
MINIO_ENDPOINT=s3.amazonaws.com
MINIO_REGION=us-east-1
MINIO_USE_SSL=true
```

**Vantagens**:
- ✅ Mais confiável e testado
- ✅ Integração com CloudFront (CDN)
- ✅ Lifecycle policies avançadas
- ✅ Versionamento de objetos

**Desvantagens**:
- ❌ Mais caro que alternativas
- ❌ Vendor lock-in

### MinIO (Self-hosted)

**Configuração**:
```env
MINIO_ENDPOINT=minio.meu-dominio.com:9000
MINIO_REGION=us-east-1
MINIO_USE_SSL=true
```

**Vantagens**:
- ✅ Gratuito (self-hosted)
- ✅ Controle total
- ✅ Compatível com S3 API
- ✅ Bom para desenvolvimento

**Desvantagens**:
- ❌ Você gerencia infraestrutura
- ❌ Requer configuração de HA

### Backblaze B2

**Configuração**:
```env
MINIO_ENDPOINT=s3.us-west-004.backblazeb2.com
MINIO_REGION=us-west-004
MINIO_USE_SSL=true
```

**Vantagens**:
- ✅ Muito mais barato que S3 (1/4 do preço)
- ✅ Egress gratuito (até 3x storage)
- ✅ S3-compatible

**Desvantagens**:
- ❌ Não suporta bucket policies (usa presigned URLs)
- ❌ Menos integrações que AWS

### DigitalOcean Spaces

**Configuração**:
```env
MINIO_ENDPOINT=nyc3.digitaloceanspaces.com
MINIO_REGION=nyc3
MINIO_USE_SSL=true
```

**Vantagens**:
- ✅ Preço fixo ($5/mês por 250GB)
- ✅ CDN incluso
- ✅ Fácil configuração

**Desvantagens**:
- ❌ Limite de 250GB no plano básico
- ❌ Menos recursos que S3

### Wasabi

**Configuração**:
```env
MINIO_ENDPOINT=s3.wasabisys.com
MINIO_REGION=us-east-1
MINIO_USE_SSL=true
```

**Vantagens**:
- ✅ Sem custo de egress
- ✅ Preço competitivo
- ✅ S3-compatible

---

## Exemplos Práticos

### 1. Upload de Mídia

Quando o WhatsApp recebe uma imagem, vídeo ou documento:
1. O WhatyGo baixa o arquivo
2. Armazena automaticamente no object storage configurado
3. Gera uma URL presignada de acesso
4. A URL é incluída na resposta da API ou evento

### 2. Acesso a Mídia Armazenada

Para acessar um arquivo já armazenado:
- Use a URL presignada retornada durante o upload
- Se a URL expirou (>7 dias), solicite uma nova através da API

### 3. Limpeza de Arquivos Antigos

Configure lifecycle policies no seu provedor para deletar automaticamente arquivos antigos:
- **AWS S3**: Configure regras de ciclo de vida no console
- **MinIO**: Use o comando `mc ilm` para configurar políticas
- **Outros**: Consulte a documentação do provedor

---

## Boas Práticas

### 1. Use Nomes de Arquivo Únicos

Gere nomes únicos para cada arquivo para evitar sobrescrever arquivos existentes:

**❌ Evite**: Usar nomes genéricos como `photo.jpg`, `video.mp4`

**✅ Recomendado**: Usar identificadores únicos como UUID ou timestamp:
- `photo-abc123-def456-ghi789.jpg`
- `video-20250111-103045.mp4`
- `document-550e8400-e29b-41d4.pdf`

### 2. Configure Content-Type Correto

O WhatyGo configura automaticamente o Content-Type baseado na extensão do arquivo:
- `.jpg`, `.jpeg` → `image/jpeg`
- `.png` → `image/png`
- `.mp4` → `video/mp4`
- `.pdf` → `application/pdf`
- `.ogg` → `audio/ogg`

### 3. Implemente Lifecycle Policies

**AWS S3**:
```json
{
  "Rules": [
    {
      "Id": "delete-old-media",
      "Status": "Enabled",
      "Prefix": "whatygo-medias/",
      "Expiration": {
        "Days": 30
      }
    }
  ]
}
```

**MinIO**:
```bash
mc ilm add local/whatygo-media   --prefix "whatygo-medias/"   --expiry-days 30
```

### 4. Use CDN para Distribuição

**CloudFront (AWS)**:
1. Crie distribuição CloudFront
2. Aponte origin para bucket S3
3. URLs ficam: `https://d123456.cloudfront.net/whatygo-medias/photo.jpg`

**DigitalOcean Spaces CDN** (automático):
```
https://bucket-name.nyc3.cdn.digitaloceanspaces.com/file.jpg
```

### 5. Monitore Uso e Custos

```bash
# AWS S3 - Tamanho total do bucket
aws s3 ls s3://bucket-name --recursive --summarize | grep "Total Size"

# MinIO
mc du local/whatygo-media
```

### 6. Comprima Imagens Quando Possível

Para reduzir custos de armazenamento e transferência:
- Comprima imagens antes de armazenar
- Use formatos modernos como WebP quando possível
- Ajuste a qualidade JPEG para 80-90% (reduz tamanho sem perda significativa de qualidade)

### 7. Monitore Uso e Custos

Acompanhe regularmente:
- Tamanho total do bucket
- Número de arquivos armazenados
- Custos mensais de armazenamento e transferência
- URLs presignadas que expiraram

---

## Troubleshooting

### Erro: "failed to create MinIO client"

**Causa**: Credenciais ou endpoint incorretos.

**Solução**:
```bash
# Testar conexão com mc
mc alias set test https://endpoint access-key secret-key
mc ls test
```

### Erro: "bucket does not exist"

**Causa**: Bucket não foi criado.

**Solução**:
```bash
# Criar bucket
mc mb test/whatygo-media

# Ou via AWS CLI
aws s3 mb s3://whatygo-media
```

### URLs retornam 403 Forbidden

**Causa 1**: URL presignada expirou (>7 dias).

**Solução**: Gere nova URL com `GetURL()`.

**Causa 2**: Bucket policy incorreta.

**Solução**: Use presigned URLs (funcionam independente de bucket policy).

### Upload muito lento

**Causa**: Latência de rede para região distante.

**Solução**:
1. Use região mais próxima do servidor
2. Use multipart upload para arquivos grandes (>5MB)
3. Configure timeout adequado

---

## Próximos Passos

- [Sistema de Eventos](./events-system.md) - Receber notificações de uploads
- [Conexão QR Code](./qrcode-connection.md) - Autenticação WhatsApp
- [Multi-Dispositivo](./multi-device.md) - Suporte Multi-Device
- [API de Mensagens](../guias-api/api-messages.md) - Enviar mídias via API

---

**Documentação gerada para WhatyGo v1.0**
