# ofertas-bot

Bot en Go que busca ofertas de **integración social en Madrid** y las manda por **Telegram**.
Solo usa la librería estándar.

```
fuentes (InfoJobs API, Hacesfalta.org, RSS del BOCM/BOAM…) → filtro por palabras clave → dedupe → Telegram
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

En la **primera ejecución real** manda un **resumen agrupado** con todas las ofertas publicadas
en ese momento (en pocos mensajes, no uno por oferta). A partir de ahí avisa solo de las nuevas.

¿Quieres volver a ver todo lo que hay publicado ahora mismo, aunque ya se haya avisado?
```bash
go run ./cmd/ofertas-bot -once -digest
```

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
| `hacesfalta` | Búsquedas en las ofertas remuneradas de Hacesfalta.org, quedándose solo con las de `province`. |
| `bulletins` | Boletines cuyo RSS solo enlaza al boletín del día (BOAM). Ver abajo. |
| `rss` | Lista de feeds RSS. Añadir una fuente nueva con RSS = añadir una entrada aquí. `label` es el nombre que se muestra y `title_from_description` usa la descripción como título (para feeds con títulos genéricos, como el BOCM). |
| `state_max_days` | Días que se recuerda una oferta ya avisada. |

### Fuentes nuevas
Cuando se añade una fuente a un bot que ya está en marcha, lo que esa fuente tenga publicado
llega en **un resumen agrupado** ("Nueva fuente: …") en vez de como alertas sueltas.

### Boletines (`bulletins`)
Para boletines cuyo RSS solo anuncia "ha salido el boletín de hoy", como el **BOAM** del
Ayuntamiento de Madrid (RSS sacado de datos.madrid.es). El bot entra en la página de los
`max_bulletins` boletines más recientes y convierte cada anuncio del sumario en una oferta
que pasa por el filtro de palabras clave.

## Estructura
```
cmd/ofertas-bot/     main: config, bucle, orquestación
internal/job/        modelo Job + interfaz Fetcher
internal/fetch/      InfoJobs, Hacesfalta.org, RSS genérico, boletines (BOAM)
internal/filter/     palabras clave
internal/store/      ofertas ya vistas (JSON; cambiable por SQLite)
internal/notify/     Telegram y salida por consola
```

## Siguientes pasos
- Más portales del tercer sector (Canal ONG…).
- Comandos en el bot (`/keywords`, `/añadir …`) para que ella ajuste filtros sin tocar código.
- Resumen diario además de las alertas sueltas.
