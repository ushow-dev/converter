# Установка и настройка Dante SOCKS5 proxy на Ubuntu

Ниже инструкция для Ubuntu 20.04/22.04/24.04.

## 1) Установка Dante

```bash
sudo apt update
sudo apt install -y dante-server
```

## 2) Создание пользователя для прокси

Если пользователя еще нет, создайте его:

```bash
sudo useradd -M -s /usr/sbin/nologin proxyuser
sudo passwd proxyuser
```

Если пользователь уже создан, просто обновите пароль:

```bash
sudo passwd proxyuser
```

Используйте пароль:

```text
EYomlhjnUV8e
```

## 3) Настройка Dante

Откройте конфиг:

```bash
sudo nano /etc/danted.conf
```

Вставьте конфигурацию (замените `eth0`, если интерфейс другой):

```conf
logoutput: syslog

internal: 0.0.0.0 port = 1080
external: eth0

socksmethod: username
user.privileged: root
user.unprivileged: nobody
user.libwrap: nobody

client pass {
    from: 0.0.0.0/0 to: 0.0.0.0/0
    log: connect disconnect error
}

socks pass {
    from: 0.0.0.0/0 to: 0.0.0.0/0
    command: connect bind udpassociate
    log: connect disconnect error
}
```

Как узнать сетевой интерфейс:

```bash
ip -br a
```

Обычно это `eth0`, `ens3`, `enp0s3` и т.п.

## 4) Разрешить порт 1080 в firewall

Если используете UFW:

```bash
sudo ufw allow 1080/tcp
sudo ufw reload
```

## 5) Запуск и автозапуск

```bash
sudo systemctl restart danted
sudo systemctl enable danted
sudo systemctl status danted --no-pager
```

Проверка логов:

```bash
sudo journalctl -u danted -n 100 --no-pager
```

## 6) Подключение к прокси

Параметры:

- Тип: `SOCKS5`
- Хост: `IP_СЕРВЕРА`
- Порт: `1080`
- Логин: `proxyuser`
- Пароль: `EYomlhjnUV8e`

### Проверка через curl

```bash
curl --proxy socks5h://proxyuser:EYomlhjnUV8e@IP_СЕРВЕРА:1080 https://api.ipify.org
```

Если прокси работает, команда вернет внешний IP сервера.

### Linux (переменная окружения)

```bash
export ALL_PROXY="socks5h://proxyuser:EYomlhjnUV8e@IP_СЕРВЕРА:1080"
curl https://api.ipify.org
```

### Браузер / приложения

В настройках сети укажите SOCKS5:

- Server: `IP_СЕРВЕРА`
- Port: `1080`
- Username: `proxyuser`
- Password: `EYomlhjnUV8e`

## 7) Быстрый чек-лист при проблемах

1. `systemctl status danted` — сервис должен быть `active (running)`.
2. Проверить интерфейс в `external:` (частая причина).
3. Проверить открытие порта `1080/tcp` в firewall/security group.
4. Проверить, что используете `socks5h`, а не `socks5` (важно для DNS через прокси).

