#!/bin/sh
set -eu

read_password() {
    password=$(cat "$1" && printf '.') || return 1
    password=${password%.}
    newline='
'
    password=${password%"$newline"}
    carriage_return=$(printf '\r')
    password=${password%"$carriage_return"}
    case "$password" in
        ''|*"$carriage_return"*|*"$newline"*) echo "Invalid database Secret: $1" >&2; return 1 ;;
    esac
    printf '%s' "$password"
}

run_sql() {
    db_password=$(read_password "$RADISHNEXUS_DATABASE_PASSWORD_FILE") || return 1
    PGPASSWORD="$db_password" PGCONNECT_TIMEOUT=10 psql --no-psqlrc --no-password --quiet \
        --set ON_ERROR_STOP=1 --dbname "$DATABASE_URL" "$@"
}

operation=$1
shift
case "$operation" in
    provision)
        if [ "$#" -ne 0 ]; then
            echo 'database-roles does not accept arguments' >&2
            exit 1
        fi
        if [ -t 0 ] || [ -t 1 ]; then
            echo 'Run database-roles with docker compose run --rm -T' >&2
            exit 1
        fi
        admin=$(read_password "$RADISHNEXUS_DATABASE_PASSWORD_FILE")
        app=$(read_password /run/secrets/app_database_password)
        owner=$(read_password /run/secrets/owner_database_password)
        backup=$(read_password /run/secrets/backup_database_password)
        for password in "$app" "$owner" "$backup"; do
            case "$password" in
                *[!a-zA-Z0-9_+/=-]*) echo 'Role Secrets must use base64 or base64url characters' >&2; exit 1 ;;
            esac
            if [ "${#password}" -lt 32 ] || [ "${#password}" -gt 128 ]; then
                echo 'Role Secrets must contain 32 to 128 characters' >&2
                exit 1
            fi
        done
        if [ "$app" = "$owner" ] || [ "$app" = "$backup" ] || [ "$owner" = "$backup" ] ||
           [ "$admin" = "$app" ] || [ "$admin" = "$owner" ] || [ "$admin" = "$backup" ]; then
            echo 'Database identities require distinct Secrets' >&2
            exit 1
        fi
        # psql's \password reads pairs from stdin and sends SCRAM verifiers,
        # keeping the plaintext out of SQL, arguments and output.
        printf '%s\n' "$owner" "$owner" "$app" "$app" "$backup" "$backup" |
            run_sql --file /usr/local/share/radishnexus/database-roles.sql
        echo 'Database roles provisioned.'
        ;;
    migrate|restore)
        "/usr/local/bin/nexus-$operation" "$@"
        run_sql --file /usr/local/share/radishnexus/database-access.sql
        echo 'Database access applied.'
        ;;
    *)
        echo 'Unknown database operation' >&2
        exit 1
        ;;
esac
