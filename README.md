# ofertas-bot

Bot en Go que busca ofertas de **integración social en Madrid** y las manda por **Telegram**.
Solo usa la librería estándar.

Está pensado para acompañar una búsqueda de empleo sin agobiar: mensajes cercanos,
solo en horario de día, un resumen agrupado en vez de ráfagas de avisos y nunca mensajes
de error ni de "hoy no hay nada" (los fallos van a los logs).

```
fuentes (alertas por email de InfoJobs/Indeed/LinkedIn, Hacesfalta.org, BOE, BOCM…) → filtro por palabras clave → dedupe → Telegram
```

## Puesta en marcha

### 1. Bot de Telegram
1. Habla con [@BotFather](https://t.me/BotFather), `/newbot` y guarda el **token**.
2. Que tu amiga abra el bot y le mande cualquier mensaje.
3. Abre `https://api.telegram.org/bot<TOKEN>/getUpdates` y copia `message.chat.id`: ese es el **chat id**.

### 2. Alertas por email (InfoJobs, Indeed, LinkedIn)
Estos portales no tienen API abierta, pero sí alertas por correo. El bot lee ese buzón y
reenvía las ofertas al grupo de Telegram.

1. Crea un Gmail solo para esto y activa la **verificación en dos pasos**.
2. Crea una **contraseña de aplicación** en <https://myaccount.google.com/apppasswords>
   (16 letras). Es la que usa el bot; tu contraseña normal no sirve ni hace falta.
3. Con ese correo, crea las alertas en InfoJobs / Indeed / LinkedIn
   ("integrador/a social", Madrid).
4. Secrets: `ALERTS_EMAIL` (la dirección) y `ALERTS_EMAIL_PASSWORD` (la contraseña de aplicación).

El bot solo **lee** (modo de solo lectura, sin marcar como leído ni borrar nada) los correos
de los últimos `email_alerts.days` días. Como los enlaces de los correos llevan tracking
distinto en cada envío, cada oferta se identifica por portal + título + entidad.

Para ajustar el parser con un correo real sin tocar el buzón ni Telegram:
en Gmail, ⋮ → *Descargar mensaje* (`.eml`) y
```bash
go run ./cmd/ofertas-bot -parse-eml ~/Downloads/alerta.eml
```

### 3. Adzuna (desactivado: es de pago)
Agregador con ofertas de muchos portales y webs de empresas. Regístrate gratis en
<https://developer.adzuna.com> y copia el **app_id** y el **app_key**
(`ADZUNA_APP_ID`, `ADZUNA_APP_KEY`). El plan gratuito permite 250 llamadas al día;
el bot hace unas 3 por pasada.

### 4. API de InfoJobs (desactivada)
A fecha de octubre de 2026 InfoJobs tiene cerrado el registro de apps nuevas. El código
está listo: si reabren, activa `infojobs.enabled` y añade las credenciales.
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
se ejecuta cada 2 horas, solo de día (aprox. 8–20 h en Madrid). Añade en *Settings → Secrets → Actions*:
`TELEGRAM_TOKEN`, `TELEGRAM_CHAT_ID`, `ALERTS_EMAIL`, `ALERTS_EMAIL_PASSWORD`.
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
| `email_alerts` | Buzón IMAP con las alertas de los portales (`imap_server`, `mailbox`, `days`). Las ofertas de las alertas ya vienen filtradas por la búsqueda guardada: solo se les aplican las exclusiones. |
| `adzuna` | Búsquedas (frase exacta) en Adzuna España, en `where`, de los últimos `max_days_old` días. |
| `hacesfalta` | Búsquedas en las ofertas remuneradas de Hacesfalta.org, quedándose solo con las de `province`. |
| `boe` | Sumario del BOE (sección II.B, oposiciones y concursos) de los últimos `days` días, vía su API de datos abiertos. Se queda con lo de Madrid (Ayuntamiento, municipios, Comunidad) y lo estatal; de los anuncios de Madrid descarga el texto para filtrar dentro. |
| `bulletins` | Boletines cuyo RSS solo enlaza al boletín del día (BOAM). Ver abajo. |
| `rss` | Lista de feeds RSS. Añadir una fuente nueva con RSS = añadir una entrada aquí. `label` es el nombre que se muestra y `title_from_description` usa la descripción como título (para feeds con títulos genéricos, como el BOCM). |
| `state_max_days` | Días que se recuerda una oferta ya avisada. |

### Fuentes nuevas
Cuando se añade una fuente a un bot que ya está en marcha, lo que esa fuente tenga publicado
llega en **un resumen agrupado** ("Nueva fuente: …") en vez de como alertas sueltas.

### Boletines (`bulletins`)
Para boletines cuyo RSS solo anuncia "ha salido el boletín de hoy". El bot entra en la página
de los `max_bulletins` boletines más recientes y convierte cada anuncio del sumario en una
oferta que pasa por el filtro de palabras clave.

> El **BOAM** no se puede usar así: `sede.madrid.es` responde 403 a clientes que no son
> navegadores. Las convocatorias del Ayuntamiento de Madrid se cubren a través del **BOE**.

## Estructura
```
cmd/ofertas-bot/     main: config, bucle, orquestación
internal/job/        modelo Job + interfaz Fetcher
internal/fetch/      Adzuna, InfoJobs, Hacesfalta.org, BOE (API), RSS genérico, boletines
internal/mailalert/  alertas por email: cliente IMAP mínimo + parser de correos
internal/filter/     palabras clave
internal/store/      ofertas ya vistas (JSON; cambiable por SQLite)
internal/notify/     Telegram y salida por consola
```

## Siguientes pasos
- Más portales del tercer sector (Canal ONG…).
- Comandos en el bot (`/keywords`, `/añadir …`) para que ella ajuste filtros sin tocar código.
- Resumen diario además de las alertas sueltas.
