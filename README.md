# Go SNMP Proxy & Packet Sniffer

Aplikasi Go SNMP UDP Sniffer / Caching Proxy yang digunakan untuk menangkap, menganalisis, dan memproses paket SNMP UDP dari OPNsense atau NMS.

---

## 🚀 Panduan Running di VM Menggunakan Docker

Berikut adalah langkah-langkah _step-by-step_ untuk memasang dan menjalankan aplikasi ini di Virtual Machine (VM) menggunakan Docker & Docker Compose.

---

### 📋 Prasyarat di VM

Sebelum memulai, pastikan VM Anda (Ubuntu/Debian/CentOS/RHEL) sudah memiliki:

1. **Git**
2. **Docker Engine**
3. **Docker Compose Plugin** (atau `docker-compose`)

> _Catatan:_ Paket SNMP berjalan melalui protokol **UDP**. Di `docker-compose.yml`, container disetting menggunakan `network_mode: host` agar dapat langsung mendengarkan lalu lintas paket UDP di port host VM tanpa hambatan NAT/bridge.

---

### 🛠️ Langkah-Langkah Deployment

#### 1. Clone / Copy Source Code ke VM

Masuk ke VM via SSH, lalu clone repository ini atau copy folder project ke direktori pilihan Anda:

```bash
git clone <URL_REPOSITORY_ANDA>
cd snmp-proxy-cache
```

#### 2. Kustomisasi Konfigurasi (Opsional)

Jika Anda ingin mengubah port yang didengarkan oleh sniffer, Anda dapat menyesuaikan environment variable `LISTEN_PORTS` pada file [docker-compose.yml](file:///docker-compose.yml):

```yaml
services:
  snmp-proxy-cache:
    build: .
    container_name: snmp-proxy-cache
    network_mode: host
    restart: always
    environment:
      - LISTEN_PORTS=21001,21002,21003,21004,21005
```

#### 3. Build & Jalankan Container

Jalankan perintah berikut di dalam direktori project untuk membangun image dan mengaktifkan container di background:

```bash
docker compose up -d --build
```

_(Jika menggunakan versi Docker Compose lama, gunakan perintah `docker-compose up -d --build`)_

---

### 📊 Monitoring & Pengelolaan Container

- **Melihat Status Container:**

  ```bash
  docker compose ps
  ```

- **Melihat Real-time Logs (Hasil Tangkapan Paket SNMP):**

  ```bash
  docker compose logs -f
  ```

- **Menghentikan Container:**

  ```bash
  docker compose stop
  ```

- **Menghentikan dan Menghapus Container:**

  ```bash
  docker compose down
  ```

- **Restart Container:**
  ```bash
  docker compose restart
  ```

---

### ⚙️ Pengaturan Firewall VM (UFW / Firewalld)

Pastikan port UDP yang didefinisikan (misal: `21001-21005`) diizinkan melalui firewall VM Anda:

- **Untuk UFW (Ubuntu/Debian):**

  ```bash
  sudo ufw allow 21001:21005/udp
  sudo ufw reload
  ```

- **Untuk Firewalld (CentOS/RHEL):**
  ```bash
  sudo firewall-cmd --permanent --add-port=21001-21005/udp
  sudo firewall-cmd --reload
  ```
