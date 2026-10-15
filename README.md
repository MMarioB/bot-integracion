# ofertas-bot

Bot en Go que busca ofertas de **integración social en Madrid** y las manda por **Telegram**.
Solo usa la librería estándar.

```
fuentes (InfoJobs API, RSS del BOCM…) → filtro por palabras clave → dedupe → Telegram
```

## Puesta en marcha

### 1. Bot de Telegram
1. Habla con [@BotFather](https://t.me/BotFather), `/newbot` y guarda el **token**.
2. Que tu amiga abra el bot y le mande cualquier mensaje.
3. Abre `https://api.telegram.org/bot<TOKEN>/getUpdates` y copia `message.chat.id`: ese es el **chat id**.

### 2. InfoJobs (opcional pero recomendable)
Registra una app en <https://developer.infojobs.net> para obtener `client_id` y `client_secret`.
Si no hay credenciales, el bot simplemente se salta InfoJobs y sigue con el resto.

> El fetcher usa `GET /api/9/offer?q=…&province=madrid`. Revisa en la doc de InfoJobs
> que la versión y los parámetros siguen siendo esos cuando tengas las credenciales.

### 3. Probar en local
```bash
go run ./cmd/ofertas-bot -once -dry-run          # imprime en consola, no guarda estado

export TELEGRAM_TOKEN=... TELEGRAM_CHAT_ID=...
export INFOJOBS_CLIENT_ID=... INFOJOBS_CLIENT_SECRET=...
go run ./cmd/ofertas-bot -once                    # una pasada real
```

En la **primera ejecución real** no manda todas las ofertas que ya existen: las marca como
vistas y envía un único mensaje de bienvenida. A partir de ahí avisa solo de las nuevas.

## Despliegue

**GitHub Actions (gratis, sin servidor).** El workflow `.github/workflows/ofertas.yml`
se ejecuta cada 2 horas en horario diurno. Añade en *Settings → Secrets → Actions*:
`TELEGRAM_TOKEN`, `TELEGRAM_CHAT_ID`, `INFOJOBS_CLIENT_ID`, `INFOJOBS_CLIENT_SECRET`.
El estado se guarda en la caché de Actions. Nota: GitHub desactiva los crons de repos sin
actividad durante 60 días; un commit de vez en cuando lo evita.

**Docker (si tienes un VPS o una Raspberry).**
```bash
docker build -t ofertas-bot .
docker run -d --name ofertas-bot --restart unless-stopped \
  -e TELEGRAM_TOKEN -e TELEGRAM_CHAT_ID -e INFOJOBS_CLIENT_ID -e INFOJOBS_CLIENT_SECRET \
  -v ofertas-data:/app/data ofertas-bot
```

## Configuración (`config.json`)

| Campo | Para qué |
|---|---|
| `keywords` | Si el título o el resumen contiene alguna, la oferta pasa. Sin distinguir mayúsculas ni tildes. |
| `exclude` | Si contiene alguna, se descarta (p. ej. voluntariado). |
| `infojobs.queries` | Búsquedas que se lanzan contra InfoJobs. |
| `rss` | Lista de feeds RSS. Añadir una fuente nueva con RSS = añadir una entrada aquí. |
| `state_max_days` | Días que se recuerda una oferta ya avisada. |

## Estructura
```
cmd/ofertas-bot/     main: config, bucle, orquestación
internal/job/        modelo Job + interfaz Fetcher
internal/fetch/      InfoJobs, RSS genérico (con soporte ISO-8859-1)
internal/filter/     palabras clave
internal/store/      ofertas ya vistas (JSON; cambiable por SQLite)
internal/notify/     Telegram y salida por consola
```

## Siguientes pasos
- Fetchers para Hacesfalta.org / Canal ONG y empleo público del Ayuntamiento de Madrid.
- Comandos en el bot (`/keywords`, `/añadir …`) para que ella ajuste filtros sin tocar código.
- Resumen diario además de las alertas sueltas.
