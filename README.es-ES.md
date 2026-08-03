

<div align="center">
  <h1>🚀 go-repo-orchestrator</h1>
  <p><b>Utilidad local TUI para orquestación de repositorios, auditoría de tareas y trabajo seguro con ramas de Git</b></p>
  
  [![Go version](https://img.shields.io/github/go-mod/go-version/AgelxNash/go-repo-orchestrator)](https://github.com/AgelxNash/go-repo-orchestrator/blob/main/go.mod)
  [![Latest release](https://img.shields.io/github/v/release/AgelxNash/go-repo-orchestrator)](https://github.com/AgelxNash/go-repo-orchestrator/releases)
  [![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-fe5196.svg)](https://www.conventionalcommits.org/en/v1.0.0/)
  ![Go Report Card](https://goreportcard.com/badge/github.com/AgelxNash/go-repo-orchestrator)
  [![CI](https://img.shields.io/github/actions/workflow/status/AgelxNash/go-repo-orchestrator/ci.yml?branch=main&label=CI)](https://github.com/AgelxNash/go-repo-orchestrator/actions/workflows/ci.yml)
  [![Release workflow](https://img.shields.io/github/actions/workflow/status/AgelxNash/go-repo-orchestrator/release.yaml?label=Release)](https://github.com/AgelxNash/go-repo-orchestrator/actions/workflows/release.yaml)
  [![Go Reference](https://pkg.go.dev/badge/github.com/AgelxNash/go-repo-orchestrator.svg)](https://pkg.go.dev/github.com/AgelxNash/go-repo-orchestrator)
  [![License](https://img.shields.io/github/license/AgelxNash/go-repo-orchestrator)](https://github.com/AgelxNash/go-repo-orchestrator/blob/main/LICENSE)
</div>

<br/>

> **go-repo-orchestrator** es su panel de control personal para gestionar el caos en los microservicios. La herramienta resuelve los problemas de un onboarding prolongado, permite realizar búsquedas transversales en las ramas, seguir los estados reales de las tareas de Jira y generar scripts seguros para eliminar ramas innecesarias dentro de una TUI interactiva.

<div align="center">
  <br>
  <img src=".github/assets/demo.gif" alt="Demostración de la aplicación" width="100%">
  <br>
  <i>Demostración de la aplicación</i>
</div>

---

## 📖 Tabla de contenidos

- [🔍 Problema y Solución](#-проблема-и-решение)
- [✨ Funciones ocultas y características](#-скрытые-фичи-и-возможности)
- [🚀 Inicio rápido](#-быстрый-старт)
- [💻 Instalación](#-установка)
- [⌨️ Atajos de teclado](#️-горячие-клавиши)
- [🏷️ Verificación automática basada en lanzamientos de Jira](#️-jira-release-driven-autocheck)
- [⚙️ Configuración](#️-конфигурация)
- [🤝 Contributing](#-contributing)

---

## 🔍 Problema y Solución

Al trabajar con decenas de microservicios, el desarrollador pierde mucho tiempo en tareas rutinarias: onboarding prolongado, cambios complejos (`cd`) entre carpetas, pérdida del contexto de los estados del rastreador de tareas y el miedo a eliminar accidentalmente una rama necesaria con el comando `git branch -D`.

**go-repo-orchestrator resuelve estos problemas al unir la gestión de todos los repositorios en una única interfaz.**

| Dolor | ❌ Enfoque tradicional | ✅ go-repo-orchestrator |
|---|---|---|
| **Onboarding prolongado** | Clonado manual de 50+ repos por carpetas | Ejecutar con un `config.yaml` común extrae y organiza automáticamente todos los proyectos |
| **Búsqueda y navegación** | `cd`, `git branch` e IDE infinitos | Búsqueda global de ramas y cambio (Checkout) con un clic o pulsando `Enter` |
| **Tareas pendientes** | Buscar estados manualmente | Monitoreo transversal de estados desde Jira directamente en la TUI (fácil de encontrar tareas canceladas) |
| **Cambio de Jira/Proyectos** | Difícil de rastrear diferentes dominios | Soporte para trabajar simultáneamente con **varias** instancias de Jira |
| **Peligro de `git branch -D`**| Miedo a eliminar código ajeno / production | Selección en TUI con vista previa, generación de script y protección regex `branch.keep` |

### 🔒 Eliminación segura por defecto

Los comandos destructivos no se ejecutan "detrás de escena". Usted selecciona las ramas en la TUI, después de lo cual se genera un script `.sh` / `.bat` que podrá analizar y ejecutar manualmente. Además, la utilidad protege por hardware la rama activa actual y las ramas del sistema (por defecto: `main|master|prod|release`).

**Principios de seguridad:**
- **Solo asistente**: La herramienta solo ayuda a encontrar candidatos a eliminación, pero nunca toma la decisión final por sí misma.
- **Sin Auto-delete**: La eliminación automática de ramas no existe como clase.
- **Prioridad de la selección manual**: Cualquier marca automática (regex o autochequeo de Jira) puede ser cancelada instantáneamente por el usuario en la TUI.
- **Sin ejecución automática silenciosa**: Las verificaciones de estado de lanzamiento nunca se inician ocultas al inicio o actualizar (refresh) — solo mediante una acción explícita del usuario.

---

## ✨ Funciones ocultas y características

- 🚀 **Onboarding instantáneo (Gestor de espacios de trabajo)**: Comparta un solo archivo YAML de configuración con el equipo. Al ejecutarse, el orquestador clonará automáticamente todos los repositorios faltantes y los organizará en las directorios correctos, ahorrando horas a los nuevos miembros.
- 👁️ **Integración profunda con Jira**:
  - El orquestador no solo monitorea si la rama se ha fusionado, sino también los **estados reales de las tareas**.
  - Comprenderá de inmediato que la tarea está "En revisión", incluso si nadie ha abierto aún la Merge Request.
  - Detección instantánea de tareas abandonadas o canceladas debido a cambios de prioridad para limpiar el desorden local.
  - **Multi-Jira**: Trabajo con varias instancias de Jira simultáneamente (ideal para situaciones donde parte de los proyectos "se mudan" a nuevos servidores).
- 🔀 **Navegación y Checkout cómodos**: Cambio de ramas activas con una sola pulsación de `Enter`. Ya no necesita "saltar" entre terminales e IDE por las carpetas.
- 🔎 **Búsqueda transversal**: Búsqueda global de las ramas y repositorios deseados directamente desde la TUI. Una función invaluable cuando, dentro de una sola tarea, modifica 5-7 repositorios diferentes.
- 🗑️ **Generación de comandos de limpieza**: Genera lotes de `git branch -D` para ramas locales y `git push <remote> --delete` para ramas remotas, respetando estrictas reglas de seguridad.
- 🏷️ **Verificación automática basada en lanzamientos de Jira**:
  - Al pulsar `*` en la pestaña de ramas, puede iniciar la marcado automático de candidatos a eliminación basándose en el estado del lanzamiento en Jira.
  - El orquestador le pedirá seleccionar uno de los lanzamientos recientes (Released), encontrará todas las tareas asociadas y marcará las ramas locales correspondientes.
  - **Solo asistente**: La automatización solo sugiere selecciones, la decisión final siempre recae en el usuario (Manual override > Auto-mark).
  - **Solo sesión**: La selección del lanzamiento se guarda solo durante la sesión actual.
  - **Resiliencia (solo release-autocheck)**: Dentro del autochequeo impulsado por lanzamientos de Jira, se admite reintentos limitados/backoff y el encabezado `Retry-After` para sortear los límites de la API de Jira.
  - **Sin ejecución automática silenciosa**: La verificación nunca se inicia automáticamente al arrancar o actualizar.
- 🌐 **Puentes CDP & Playwright**: Transporte adicional a través de Chromium para grupos de Jira complejos con protección (Cloudflare/Captchas).

---

## 🚀 Inicio rápido

**Requisitos:**

- Go `1.24+`
- `git` instalado en `$PATH`

Existen dos escenarios principales para trabajar con la configuración del orquestador:

### Escenario 1: Con configuración lista (Onboarding)

Ideal si en su equipo ya existe un archivo de configuración preparado. El orquestador descargará automáticamente (vía `git clone`) los repositorios faltantes por URL en la estructura adecuada.

```bash
# 1. Descargamos la configuración (usaremos una plantilla base como ejemplo)
curl -O https://raw.githubusercontent.com/AgelxNash/go-repo-orchestrator/main/config.example.yaml

# 2. Ejecutamos el orquestador
go run ./cmd/go-repo-orchestrator --config config.example.yaml
```

### Escenario 2: Generación de una nueva configuración

Si está implementando la utilidad en su proyecto desde cero, cree una plantilla limpia para completar:

```bash
# 1. Generamos un nuevo archivo
go run ./cmd/go-repo-orchestrator generate --config ./my-repo.gbc.yaml

# 2. Edite my-repo.gbc.yaml, agregando sus rutas y configuraciones.

# 3. Ejecute el orquestador
go run ./cmd/go-repo-orchestrator --config ./my-repo.gbc.yaml
```

Para ver la ayuda completa de la CLI:

```bash
go run ./cmd/go-repo-orchestrator --help
```

---

## 💻 Instalación

**Desde fuentes:**

```bash
make build
./bin/go-repo-orchestrator --config ./config.example.yaml
```

**Vía go install:**

```bash
go install github.com/agelxnash/go-repo-orchestrator/cmd/go-repo-orchestrator@latest
```

*Para descargar los binarios compilados, visite la sección [GitHub Releases](https://github.com/AgelxNash/go-repo-orchestrator/releases).*

---

## ⌨️ Atajos de teclado

#### Principales

- **`F2`** — Mostrar/ocultar el panel inferior de información.
- **`F3`** — Búsqueda (Global en ramas y proyectos).
- **`F4`** — Área de ramas (`Locales` → `Remotas` → `Todas`).
- **`F5`** (o `r`) — Actualizar el contexto de la pestaña actual.
- **`F6`** — Ordenar.
- **`F8`** (o `g`) — Generar script.
- **`F10`** (o `q` / `Ctrl+C`) — Salir de la aplicación.

#### Pestaña "Repositorios"

- **`Enter`**: Abrir ramas para el repositorio activo.
- **`F7`**: Ejecutar `fetch + pull` para el repositorio activo.

#### Pestaña "Ramas"

- **`Enter`**: Realizar `checkout` en la rama seleccionada sin necesidad de abrir un terminal.
- **`Espacio`** / **`Insert`**: Marcar la rama para el script de eliminación.
- **`F7`**: Crear una copia de seguimiento local de una rama remota (si el repositorio no está en modo solo-`url`).
- **`F9`**: Ocultar/mostrar ramas protegidas.
- **`*`**: Iniciar verificación automática basada en lanzamientos de Jira.
- **`+`**: Invertir la selección de ramas (seleccionar todas las no seleccionadas, deseleccionar las seleccionadas).

---

## 🏷️ Verificación automática basada en lanzamientos de Jira

Esta función permite encontrar y marcar rápidamente las ramas asociadas a tareas que ya forman parte de un lanzamiento específico de Jira.

**Proceso de trabajo:**
1. Presione **`*`** en la pestaña de ramas.
2. El orquestador cargará la lista de lanzamientos recientes (Released) desde Jira.
3. Seleccione el lanzamiento deseado en la ventana modal.
4. La herramienta encontrará todas las tareas en estado "Done" vinculadas a ese lanzamiento.
5. Mediante el mecanismo de coincidencia `branch.jira`, el orquestador marcará automáticamente las ramas locales correspondientes como candidatas a eliminación.

**Garantías y limitaciones:**
- **Solo asistente**: Es solo una sugerencia. Siempre puede desmarcar manualmente antes de generar el script.
- **Sin Auto-delete**: Las ramas no se eliminan automáticamente, solo se marcan para incluir en el script.
- **Sin ejecución automática silenciosa**: Las solicitudes a la API de Jira para verificar lanzamientos se realizan solo al pulsar explícitamente `*`.
- **Solo sesión**: El lanzamiento seleccionado y los resultados de la verificación se almacenan solo hasta que se cierre la aplicación.
- **Resiliencia**: Al recibir un error 429 (Too Many Requests), el orquestador utiliza una estrategia de reintentos limitados/backoff teniendo en cuenta el encabezado `Retry-After`. Puede interrumpir la espera en cualquier momento.

---

## ⚙️ Configuración

Todos los valores predeterminados y un ejemplo de relleno se encuentran en la plantilla `config.example.yaml`, que puede generar con el comando `generate` (ver Inicio rápido). Es obligatorio pasar el archivo de configuración mediante la bandera `--config`.

**Secciones clave de la configuración:**

- `repos[].name`, `repos[].url`, `repos[].path` — configuración básica del repositorio (soporta `url`, `path`, `url+path`).
- `repos[].branch.keep` — expresión regex para ramas del sistema/protegidas.
- `repos[].branch.jira` — expresión regex para extraer la clave de Jira (por ejemplo, `[A-Z]+-\d+`).
- `jira[]` — configuración de la integración con Jira:
  - `group` — grupo/proyecto en Jira (por ejemplo, `"MYPROJ"`).
  - `url` — URL base de Jira (por ejemplo, `"https://company.atlassian.net"`).
  - `playwright` — booleano: `true` habilita el transporte por navegador a través de CDP/Chromium; `false` (predeterminado) usa transporte HTTP.
  - `token` — token API para autenticación Bearer (si se especifica, se agrega el encabezado `Authorization: Bearer <token>`).
  - `login.username` / `login.password` — credenciales para Basic Auth (se usan si ambos campos están definidos y `token` está vacío).
  - `type` — campo de configuración auxiliar que no afecta la lógica de runtime (puede usarse para documentación o extensiones futuras).
- `browser.cdp_url` — URL para conectarse a un Chromium ya iniciado a través de CDP (por ejemplo, `"http://localhost:9222"`). Se usa cuando `playwright: true`.

### Vinculación de grupos de Jira con ramas

El orquestador admite varias instancias de Jira simultáneamente. Para determinar a qué grupo de Jira pertenece una rama, se utiliza el mecanismo de grupos de captura con nombre (named capture groups) en la regex de `repos[].branch.jira`. Este mismo mecanismo se usa para el mapeo automático de ramas con tareas al ejecutar la `verificación automática basada en lanzamientos de Jira`.

**¿Cómo funciona?**

- `jira[].group` — identificador del grupo/instancia de Jira (por ejemplo, `"MARIADB"`, `"SIMPLEWINE"`).
- En la regex para `branch.jira` puede definir un named-group cuyo nombre **coincida** con `jira.group`. Cuando se extrae la clave del ticket del nombre de la rama, el orquestador encuentra el grupo de Jira correspondiente por el nombre del named-group.
- Si en la regex se usa un nombre universal `(?P<JIRA>...)`, la clave del ticket se buscará en **todos** los grupos de Jira configurados (opción de fallback).

**Ejemplos:**

```yaml
# Mapeo directo: named-group "SIMPLEWINE" -> jira.group: "SIMPLEWINE"
jira:
  - group: "SIMPLEWINE"
    url: "https://simplewine.atlassian.net"
    token: "..."
repos:
  - name: "My Service"
    branch:
      jira:
        - '(?P<SIMPLEWINE>SW-\d+)'  # La clave "SW-123" encontrará el grupo SIMPLEWINE
```

```yaml
# Fallback: named-group universal "JIRA" funciona con cualquier grupo
jira:
  - group: "PROJ-A"
    url: "https://proj-a.atlassian.net"
  - group: "PROJ-B"
    url: "https://proj-b.atlassian.net"
repos:
  - name: "Shared Lib"
    branch:
      jira:
        - '(?P<JIRA>[A-Z]+-\d+)'  # La clave "PROJ-123" verificará ambos grupos
```

**¿Por qué `generate` no rellena estos campos automáticamente?**

El comando `generate` crea una plantilla de configuración básica, pero no puede adivinar:
- Qué instancias de Jira usa su equipo (URLs, tokens, nombres de grupo).
- Cómo nombra exactamente sus ramas (prefijos, separadores, formatos de clave).
- Qué nombres de named-group son preferibles para su caso.

Estos parámetros requieren configuración manual según la organización y workflow específicos.

*Puede anular la configuración a través de variables de entorno con el prefijo `GBC_` (por ejemplo, `GBC_STATE_DIR`).*

### Autenticación de Jira

La integración con Jira admite dos modos de transporte, gestionados por el campo `playwright`:

- **Transporte por navegador** (`playwright: true`) — para SSO/cenarios complejos con Cloudflare/Captchas. El usuario se autentica manualmente en el navegador/Chromium que se abre. Tras expirar la sesión SSO, se requiere volver a iniciar sesión. Se recomienda usar `browser.cdp_url` para conectarse a una sesión CDP ya iniciada.
- **Transporte HTTP** (`playwright: false` o ausencia del campo) — solicitudes HTTP estándar a la API REST de Jira.

Para la autenticación en el modo HTTP se utilizan:
- **Autenticación Bearer** — si está definido el campo `token`, se agrega el encabezado `Authorization: Bearer <token>`.
- **Autenticación Basic** — si están definidos `login.username` y `login.password` (y `token` está vacío), se usa autenticación básica.

Para las solicitudes dentro de la `verificación automática basada en lanzamientos de Jira`, el orquestador admite la estrategia de reintentos limitados/backoff y maneja correctamente el encabezado `Retry-After` al recibir un error 429 (Too Many Requests).

El campo `type` es auxiliar y no afecta la lógica de runtime (puede usarse para documentación).

**Fallback y sugerencias:** Si el runtime del navegador no está disponible (por ejemplo, falta Chromium) para grupos con `playwright: true`, la aplicación hace automáticamente un fallback HTTP. Si Jira requiere autenticación por navegador, la TUI mostrará una sugerencia correspondiente con instrucciones.

#### Ejemplos de configuración

**SSO/Playwright (con CDP):**

```yaml
jira:
  - group: "MYPROJ"
    url: "https://company.atlassian.net"
    playwright: true

browser:
  cdp_url: "http://localhost:9222"
```

**HTTP con token Bearer:**

```yaml
jira:
  - group: "MYPROJ"
    url: "https://company.atlassian.net"
    token: "your_api_token_here"
```

**HTTP con Basic Auth:**

```yaml
jira:
  - group: "MYPROJ"
    url: "https://company.atlassian.net"
    login:
      username: "user@example.com"
      password: "secret"
```

#### Cómo ejecutar el navegador con CDP

Al usar `playwright: true`, el orquestador se conecta a un navegador ya iniciado a través de CDP. Inicie Chromium/Chrome en una sesión separada con depuración remota habilitada:

**Linux/macOS:**

```bash
# Creamos un perfil separado para no interferir con el navegador principal
PROFILE_DIR="$HOME/.config/go-repo-orchestrator-chrome-profile"

# Iniciamos Chrome/Chromium con CDP
google-chrome \
  --remote-debugging-port=9222 \
  --remote-debugging-address=127.0.0.1 \
  --user-data-dir="$PROFILE_DIR" \
  --no-first-run \
  --no-default-browser-check \
  "https://company.atlassian.net" &
```

O para `chromium`:

```bash
chromium-browser \
  --remote-debugging-port=9222 \
  --remote-debugging-address=127.0.0.1 \
  --user-data-dir="$HOME/.config/go-repo-orchestrator-chrome-profile" \
  --no-first-run \
  --no-default-browser-check \
  "https://company.atlassian.net" &
```

**Windows (PowerShell):**

```powershell
# Perfil separado
$env:PROFILE_DIR = "$env:APPDATA\go-repo-orchestrator-chrome-profile"

# Iniciar Chrome (la ruta puede variar según el sistema)
& "C:\Program Files\Google\Chrome\Application\chrome.exe" `
  --remote-debugging-port=9222 `
  --remote-debugging-address=127.0.0.1 `
  --user-data-dir="$env:PROFILE_DIR" `
  --no-first-run `
  --no-default-browser-check `
  "https://company.atlassian.net"
```

**¿Por qué un `--user-data-dir` separado?** Para que el navegador ejecutado para CDP no entre en conflicto con su perfil principal de Chrome/Chromium (extensiones, historial, cookies). Tras finalizar el trabajo con el orquestador, puede eliminar esta carpeta de forma segura.

Asegúrese de que `browser.cdp_url` en la configuración apunte a `http://localhost:9222` (valor predeterminado).

### Directorio de estado

La utilidad almacena su estado (y los repositorios del espacio de trabajo descargados) en:

- **Linux/macOS:** `$HOME/.local/state/go-repo-orchestrator`
- **Fallback:** `.go-repo-orchestrator-state`

Los espacios de trabajo clonados se almacenan en la ruta: `<state-dir>/workspace/<repo-name>__<url-hash>/`.

### Sobre la generación de scripts de limpieza

Los `.sh`/`.bat` generados se crean en su directorio de trabajo actual del terminal en el formato:

- `go-repo-orchestrator-<repo>-delete-<session>-<timestamp>.sh`

Es importante entender que los mecanismos de marcado automático (regex-autocheck y verificación basada en lanzamientos de Jira) solo forman una selección preliminar de candidatos en la TUI. La decisión final sobre la eliminación siempre recae en el usuario al ejecutar el script generado.

---

## 🤝 Contributing

¡Agradeceremos su contribución! Para familiarizarse con las reglas de *Conventional Commits*, los requisitos para *Pull Requests* y las verificaciones obligatorias, por favor lea [CONTRIBUTING.md](CONTRIBUTING.md).

<details>
<summary><b>🛠️ Preparación del entorno (Onboarding)</b></summary>

Onboarding rápido antes del primer commit:

```bash
make commitlint-install
make golangci-lint-install
make setup-hooks
```

*Los comandos instalarán las utilidades necesarias (`commitlint`, `golangci-lint`) y configurarán `core.hooksPath=.githooks` para las puertas de calidad locales (`pre-commit` y `pre-push`).*

**Verificaciones locales rápidas (llamada manual):**

```bash
make fmt-check
make vet
make check
```

</details>

<details>
<summary><b>📦 Información de lanzamientos (Para Mantenedores)</b></summary>

El workflow de lanzamiento se encuentra en `.github/workflows/release.yaml`.

- **Disparador:** push de una etiqueta con prefijo `v*`.
- Se usa `GoReleaser` + `GPG signing` del archivo de checksums.

**Verificación de la firma de los artefactos descargados en el lanzamiento:**

```bash
gpg --verify checksums.txt.sig checksums.txt
sha256sum -c checksums.txt
```

</details>

---

## 📈 Hoja de ruta

- Introducción de una capa completa de i18n para CLI/TUI y mensajes de usuario.
- Opción potencial de apertura automática del IDE (VS Code / JetBrains) para el repositorio y/o rama seleccionados desde la interfaz.

---

## ⭐ Historial de estrellas

<div align="center">
  <a href="https://www.star-history.com/?repos=AgelxNash%2Fgo-repo-orchestrator&type=date&legend=top-left">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/image?repos=AgelxNash/go-repo-orchestrator&type=date&theme=dark&legend=top-left" />
      <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/image?repos=AgelxNash/go-repo-orchestrator&type=date&legend=top-left" />
      <img alt="Gráfico de historial de estrellas" src="https://api.star-history.com/image?repos=AgelxNash/go-repo-orchestrator&type=date&legend=top-left" />
    </picture>
  </a>
</div>

## 👥 Contribuidores

<div align="center">
  <a href="https://github.com/AgelxNash/go-repo-orchestrator/graphs/contributors">
    <img src="https://contrib.rocks/image?repo=AgelxNash/go-repo-orchestrator" alt="Contribuidores" />
  </a>
  <br/>
  <i>Hecho con <a href="https://contrib.rocks">contrib.rocks</a>.</i>
</div>

---

## 📄 Licencia

**MIT** © [AgelxNash](https://github.com/AgelxNash)

<div align="center">
  <br/>
  <a href="https://github.com/AgelxNash/go-repo-orchestrator">
    <img src="https://github-view-counter.vercel.app/api?username=AgelxNash/go-repo-orchestrator&label=views&color=0969da&labelColor=555555" alt="Views"/>
  </a>
</div>
