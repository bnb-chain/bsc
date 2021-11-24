wait_for_host_port() {
    while ! nc -v -z -w 3 ${1} ${2} >/dev/null 2>&1 < /dev/null; do
        echo "$(date) - waiting for ${1}:${2}..."
        sleep 5
    done
}

get_host_ip() {
    local host_ip
    while [ -z ${host_ip} ]; do
        host_ip=$(getent hosts ${1}| awk '{ print $1 ; exit }')
    done
    echo $host_ip
}
