#!/bin/bash

# ==========================================================
# X-Panel 安装脚本（已移除授权码激活，安装后即为完整功能版）
# 作者: X-Panel
# ==========================================================

# 默认使用已去激活的仓库；如换成自己的 fork，改这里或设同名环境变量
XPANEL_REPO="${XPANEL_REPO:-v200dd/X-Panel}"
XPANEL_BRANCH="${XPANEL_BRANCH:-main}"
# 1=源码构建（最保险）；0=直接下载 Release 二进制（更快，失败会自动回退源码构建）
XUI_FROM_SOURCE="${XUI_FROM_SOURCE:-0}"

red='\033[0;31m'
green='\033[0;32m'
blue='\033[0;34m'
yellow='\033[0;33m'
plain='\033[0m'

# check root
[[ $EUID -ne 0 ]] && echo -e "${red}致命错误: ${plain} 请使用 root 权限运行此脚本\n" && exit 1


# ----------------------------------------------------------
# 从源码构建安装包（XUI_FROM_SOURCE=1 时使用）
# 用途：直接安装本仓库源码构建的完整功能版，无需等待 Release 二进制
# ----------------------------------------------------------
build_from_source_tarball() {
    echo ""
    echo -e "${green}==> 使用【源码构建】模式安装（自动编译当前仓库源码）${plain}"
    echo ""

    install_base >/dev/null 2>&1 || true

    case "${release}" in
        ubuntu | debian | armbian)
            apt-get install -y -q gcc libc6-dev git unzip ca-certificates curl >/dev/null 2>&1
            ;;
        centos | rhel | almalinux | rocky | ol | fedora | amzn)
            yum install -y -q gcc glibc-devel git unzip ca-certificates curl >/dev/null 2>&1
            ;;
        alpine)
            apk add --no-cache gcc musl-dev git unzip ca-certificates curl >/dev/null 2>&1
            ;;
    esac

    # go.mod 要求 Go 1.27+，系统源里的版本通常过旧，这里统一用官方二进制
    local go_need="1.27"
    local go_have=""
    if command -v go >/dev/null 2>&1; then
        go_have=$(go env GOVERSION 2>/dev/null | sed 's/^go//')
    fi
    if [[ -z "${go_have}" || "$(printf '%s\n%s\n' "${go_need}" "${go_have}" | sort -V | head -n1)" != "${go_need}" ]]; then
        local go_ver go_arch
        go_ver=$(curl -fsL "https://go.dev/VERSION?m=text" | head -n1)
        case "$(arch)" in
            amd64) go_arch="amd64" ;;
            arm64) go_arch="arm64" ;;
            386)   go_arch="386" ;;
            armv6 | armv7) go_arch="armv6l" ;;
            *)     go_arch="amd64" ;;
        esac
        echo -e "${green}正在安装 ${go_ver}（linux-${go_arch}）......${plain}"
        if ! curl -fL --connect-timeout 20 -o /tmp/go.tgz "https://go.dev/dl/${go_ver}.linux-${go_arch}.tar.gz"; then
            echo -e "${red}Go 下载失败，请检查网络${plain}"
            exit 1
        fi
        rm -rf /usr/local/go
        tar -C /usr/local -xzf /tmp/go.tgz
        rm -f /tmp/go.tgz
        export PATH="/usr/local/go/bin:${PATH}"
    fi

    if ! command -v go >/dev/null 2>&1; then
        echo -e "${red}未找到 go 编译器，无法从源码构建${plain}"
        exit 1
    fi
    echo -e "${green}Go 版本：$(go version)${plain}"

    local work src
    work=$(mktemp -d)

    # 优先使用脚本同目录下的源码（把改好的源码上传到 VPS 后直接执行脚本即可）
    local script_dir
    script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd)
    if [[ -n "${XUI_SRC_DIR}" && -f "${XUI_SRC_DIR}/main.go" ]]; then
        src="${XUI_SRC_DIR}"
        echo -e "${green}使用指定源码目录：${src}${plain}"
    elif [[ -n "${script_dir}" && -f "${script_dir}/main.go" ]]; then
        src="${script_dir}"
        echo -e "${green}使用脚本同目录源码：${src}${plain}"
    else
        echo -e "${green}正在下载源码：${XPANEL_REPO}@${XPANEL_BRANCH}${plain}"
        if ! curl -fL --connect-timeout 20 -o "${work}/src.zip" "https://codeload.github.com/${XPANEL_REPO}/zip/refs/heads/${XPANEL_BRANCH}"; then
            echo -e "${red}源码下载失败，请检查网络或仓库名${plain}"
            exit 1
        fi
        unzip -q "${work}/src.zip" -d "${work}"
        src=$(find "${work}" -maxdepth 1 -type d -name '*-*' ! -path "${work}" | head -n 1)
    fi
    if [[ -z "${src}" || ! -f "${src}/main.go" ]]; then
        echo -e "${red}源码异常，未找到 main.go${plain}"
        exit 1
    fi

    echo -e "${green}正在编译（约 1-3 分钟）......${plain}"
    if ! ( cd "${src}" && CGO_ENABLED=1 go build -ldflags "-w -s" -o x-ui main.go ); then
        echo -e "${red}编译失败，请确认系统内存足够（建议 1G 以上）${plain}"
        exit 1
    fi

    # 组装安装包结构
    mkdir -p "${work}/pkg/x-ui/bin"
    cp "${src}/x-ui" "${work}/pkg/x-ui/x-ui"
    cp "${src}/x-ui.service" "${work}/pkg/x-ui/x-ui.service"
    cp "${src}/x-ui.sh" "${work}/pkg/x-ui/x-ui.sh"

    local xarch xray_zip
    case "$(arch)" in
        amd64) xarch="amd64"; xray_zip="Xray-linux-64.zip" ;;
        arm64) xarch="arm64"; xray_zip="Xray-linux-arm64-v8a.zip" ;;
        armv7) xarch="armv7"; xray_zip="Xray-linux-arm32-v7a.zip" ;;
        armv6) xarch="armv6"; xray_zip="Xray-linux-arm32-v6.zip" ;;
        386)   xarch="386";   xray_zip="Xray-linux-32.zip" ;;
        *)     xarch="amd64"; xray_zip="Xray-linux-64.zip" ;;
    esac

    echo -e "${green}正在下载 Xray 内核与规则库......${plain}"
    ( cd "${work}/pkg/x-ui/bin" \
        && curl -fL -o xray.zip "https://github.com/XTLS/Xray-core/releases/download/v26.9.9/${xray_zip}" \
        && unzip -q -o xray.zip xray \
        && mv xray "xray-linux-${xarch}" \
        && rm -f xray.zip \
        && curl -fsL -o geoip.dat "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat" \
        && curl -fsL -o geosite.dat "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat" ) || {
        echo -e "${red}Xray 内核下载失败，请检查网络${plain}"
        exit 1
    }
    chmod +x "${work}/pkg/x-ui/x-ui" "${work}/pkg/x-ui/bin/xray-linux-${xarch}"

    tar -czf "/usr/local/x-ui-linux-$(arch).tar.gz" -C "${work}/pkg" x-ui
    rm -rf "${work}"
    echo -e "${green}==> 安装包构建完成${plain}"
    echo ""
}

# ----------------------------------------------------------
# 函数：完整版安装逻辑 (install_free_version) 
# ----------------------------------------------------------
install_free_version() {
    echo ""
    echo -e "${green}您正在安装 【X-Panel 完整版（已去激活）】${plain}"
    echo ""
    echo -e "${green}即将开始执行标准安装流程...${plain}"
    sleep 2

    cur_dir=$(pwd)

    # Check OS and set release variable
    if [[ -f /etc/os-release ]]; then
        source /etc/os-release
        release=$ID
    elif [[ -f /usr/lib/os-release ]]; then
        source /usr/lib/os-release
        release=$ID
    else
        echo ""
        echo -e "${red}检查服务器操作系统失败，请联系作者!${plain}" >&2
        exit 1
    fi
    echo ""
    echo -e "${green}---------->>>>>目前服务器的操作系统为: $release${plain}"

    arch() {
        case "$(uname -m)" in
            x86_64 | x64 | amd64 ) echo 'amd64' ;;
            i*86 | x86 ) echo '386' ;;
            armv8* | armv8 | arm64 | aarch64 ) echo 'arm64' ;;
            armv7* | armv7 | arm ) echo 'armv7' ;;
            armv6* | armv6 ) echo 'armv6' ;;
            armv5* | armv5 ) echo 'armv5' ;;
            s390x) echo 's390x' ;;
            *) echo -e "${green}不支持的CPU架构! ${plain}" && rm -f install.sh && exit 1 ;;
        esac
    }

    echo ""
    # check_glibc_version() {
    #    glibc_version=$(ldd --version | head -n1 | awk '{print $NF}')

    #    required_version="2.32"
    #    if [[ "$(printf '%s\n' "$required_version" "$glibc_version" | sort -V | head -n1)" != "$required_version" ]]; then
    #        echo -e "${red}------>>>GLIBC版本 $glibc_version 太旧了！ 要求2.32或以上版本${plain}"
    #        echo -e "${green}-------->>>>请升级到较新版本的操作系统以便获取更高版本的GLIBC${plain}"
    #        exit 1
    #    fi
    #        echo -e "${green}-------->>>>GLIBC版本： $glibc_version（符合高于2.32的要求）${plain}"
    # }
    # check_glibc_version

    # echo ""
    echo -e "${yellow}---------->>>>>当前系统的架构为: $(arch)${plain}"
    echo ""
    last_version=$(curl -Ls "https://api.github.com/repos/${XPANEL_REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    # 获取 x-ui 版本
    xui_version=$(/usr/local/x-ui/x-ui -v)

    # 检查 xui_version 是否为空
    if [[ -z "$xui_version" ]]; then
        echo ""
        echo -e "${red}------>>>当前服务器没有安装任何 x-ui 系列代理面板${plain}"
        echo ""
        echo -e "${green}-------->>>>片刻之后脚本将会自动引导安装〔X-Panel面板〕${plain}"
    else
        # 检查版本号中是否包含冒号
        if [[ "$xui_version" == *:* ]]; then
            echo -e "${green}---------->>>>>当前代理面板的版本为: ${red}其他 x-ui 分支版本${plain}"
            echo ""
            echo -e "${green}-------->>>>片刻之后脚本将会自动引导安装〔X-Panel面板〕${plain}"
        else
            echo -e "${green}---------->>>>>当前代理面板的版本为: ${red}〔X-Panel面板〕v${xui_version}${plain}"
        fi
    fi
    echo ""
    echo -e "${yellow}---------------------->>>>>〔X-Panel面板〕最新版为：${last_version}${plain}"
    sleep 4

    os_version=$(grep -i version_id /etc/os-release | cut -d \" -f2 | cut -d . -f1)

    if [[ "${release}" == "arch" ]]; then
        echo "您的操作系统是 ArchLinux"
    elif [[ "${release}" == "manjaro" ]]; then
        echo "您的操作系统是 Manjaro"
    elif [[ "${release}" == "armbian" ]]; then
        echo "您的操作系统是 Armbian"
    elif [[ "${release}" == "alpine" ]]; then
        echo "您的操作系统是 Alpine Linux"
    elif [[ "${release}" == "opensuse-tumbleweed" ]]; then
        echo "您的操作系统是 OpenSUSE Tumbleweed"
    elif [[ "${release}" == "centos" ]]; then
        if [[ ${os_version} -lt 8 ]]; then
            echo -e "${red} 请使用 CentOS 8 或更高版本 ${plain}\n" && exit 1
        fi
    elif [[ "${release}" == "ubuntu" ]]; then
        if [[ ${os_version} -lt 20 ]]; then
            echo -e "${red} 请使用 Ubuntu 20 或更高版本!${plain}\n" && exit 1
        fi
    elif [[ "${release}" == "fedora" ]]; then
        if [[ ${os_version} -lt 36 ]]; then
            echo -e "${red} 请使用 Fedora 36 或更高版本!${plain}\n" && exit 1
        fi
    elif [[ "${release}" == "debian" ]]; then
        if [[ ${os_version} -lt 11 ]]; then
            echo -e "${red} 请使用 Debian 11 或更高版本 ${plain}\n" && exit 1
        fi
    elif [[ "${release}" == "almalinux" ]]; then
        if [[ ${os_version} -lt 9 ]]; then
            echo -e "${red} 请使用 AlmaLinux 9 或更高版本 ${plain}\n" && exit 1
        fi
    elif [[ "${release}" == "rocky" ]]; then
        if [[ ${os_version} -lt 9 ]]; then
            echo -e "${red} 请使用 RockyLinux 9 或更高版本 ${plain}\n" && exit 1
        fi
    elif [[ "${release}" == "oracle" ]]; then
        if [[ ${os_version} -lt 8 ]]; then
            echo -e "${red} 请使用 Oracle Linux 8 或更高版本 ${plain}\n" && exit 1
        fi
    else
        echo -e "${red}此脚本不支持您的操作系统。${plain}\n"
        echo "请确保您使用的是以下受支持的操作系统之一："
        echo "- Ubuntu 20.04+"
        echo "- Debian 11+"
        echo "- CentOS 8+"
        echo "- Fedora 36+"
        echo "- Arch Linux"
        echo "- Manjaro"
        echo "- Armbian"
        echo "- Alpine Linux"
        echo "- AlmaLinux 9+"
        echo "- Rocky Linux 9+"
        echo "- Oracle Linux 8+"
        echo "- OpenSUSE Tumbleweed"
        exit 1

    fi

    install_base() {
        case "${release}" in
        ubuntu | debian | armbian)
            apt-get update && apt-get install -y -q wget curl sudo tar tzdata
            ;;
        centos | rhel | almalinux | rocky | ol)
            yum -y --exclude=kernel* update && yum install -y -q wget curl sudo tar tzdata
            ;;
        fedora | amzn | virtuozzo)
            dnf -y --exclude=kernel* update && dnf install -y -q wget curl sudo tar tzdata
            ;;
        arch | manjaro | parch)
            pacman -Sy && pacman -S --noconfirm wget curl sudo tar tzdata
            ;;
        alpine)
            apk update && apk add --no-cache wget curl sudo tar tzdata
            ;;
        opensuse-tumbleweed)
            zypper refresh && zypper -q install -y wget curl sudo tar timezone
            ;;
        *)
            apt-get update && apt-get install -y -q wget curl sudo tar tzdata
            ;;
        esac
    }

    gen_random_string() {
        local length="$1"
        local random_string=$(LC_ALL=C tr -dc 'a-zA-Z0-9' </dev/urandom | fold -w "$length" | head -n 1)
        echo "$random_string"
    }

    # This function will be called when user installed x-ui out of security
    config_after_install() {
        echo -e "${yellow}安装/更新完成！ 为了您的面板安全，建议修改面板设置 ${plain}"
        echo ""
        read -p "$(echo -e "${green}想继续修改吗？${red}选择“n”以保留旧设置${plain} [y/n]？--->>请输入：")" config_confirm
        if [[ "${config_confirm}" == "y" || "${config_confirm}" == "Y" ]]; then
            read -p "请设置您的用户名: " config_account
            echo -e "${yellow}您的用户名将是: ${config_account}${plain}"
            read -p "请设置您的密码: " config_password
            echo -e "${yellow}您的密码将是: ${config_password}${plain}"
            read -p "请设置面板端口: " config_port
            echo -e "${yellow}您的面板端口号为: ${config_port}${plain}"
            read -p "请设置面板登录访问路径: " config_webBasePath
            echo -e "${yellow}您的面板访问路径为: ${config_webBasePath}${plain}"
            echo -e "${yellow}正在初始化，请稍候...${plain}"
            /usr/local/x-ui/x-ui setting -username ${config_account} -password ${config_password}
            echo -e "${yellow}用户名和密码设置成功!${plain}"
            /usr/local/x-ui/x-ui setting -port ${config_port}
            echo -e "${yellow}面板端口号设置成功!${plain}"
            /usr/local/x-ui/x-ui setting -webBasePath ${config_webBasePath}
            echo -e "${yellow}面板登录访问路径设置成功!${plain}"
            echo ""
        else
            echo ""
            sleep 1
            echo -e "${red}--------------->>>>Cancel...--------------->>>>>>>取消修改...${plain}"
            echo ""
            if [[ ! -f "/etc/x-ui/x-ui.db" ]]; then
                local usernameTemp=$(head -c 10 /dev/urandom | base64)
                local passwordTemp=$(head -c 10 /dev/urandom | base64)
                local webBasePathTemp=$(gen_random_string 15)
                /usr/local/x-ui/x-ui setting -username ${usernameTemp} -password ${passwordTemp} -webBasePath ${webBasePathTemp}
                echo ""
                echo -e "${yellow}检测到为全新安装，出于安全考虑将生成随机登录信息:${plain}"
                echo -e "###############################################"
                echo -e "${green}用户名: ${usernameTemp}${plain}"
                echo -e "${green}密  码: ${passwordTemp}${plain}"
                echo -e "${green}访问路径: ${webBasePathTemp}${plain}"
                echo -e "###############################################"
                echo -e "${green}如果您忘记了登录信息，可以在安装后通过 x-ui 命令然后输入${red}数字 10 选项${green}进行查看${plain}"
            else
                echo -e "${green}此次操作属于版本升级，保留之前旧设置项，登录方式保持不变${plain}"
                echo ""
                echo -e "${green}如果您忘记了登录信息，您可以通过 x-ui 命令然后输入${red}数字 10 选项${green}进行查看${plain}"
                echo ""
                echo ""
            fi
        fi
        sleep 1
        echo -e ">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>"
        echo ""
        /usr/local/x-ui/x-ui migrate
    }

    echo ""
    install_x-ui() {
        cd /usr/local/

        # 源码构建模式：先在本机编译安装包，再走后续安装流程
        if [[ "${XUI_FROM_SOURCE}" == "1" && -z "$1" ]]; then
            build_from_source_tarball
            last_version="源码构建"
            local SRC_BUILD=1
        fi

        # Download resources
        if [ $# == 0 ]; then
            last_version=$(curl -Ls "https://api.github.com/repos/${XPANEL_REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
            if [[ ! -n "$last_version" && "${XUI_FROM_SOURCE}" != "1" ]]; then
                echo -e "${red}获取 X-Panel 版本失败，可能是 Github API 限制，请稍后再试${plain}"
                exit 1
            fi
            if [[ "${XUI_FROM_SOURCE}" == "1" ]]; then
                last_version="源码构建"
                SRC_BUILD=1
            fi
            echo ""
            echo -e "-----------------------------------------------------"
            echo -e "${green}--------->>获取 X-Panel 最新版本：${yellow}${last_version}${plain}${green}，开始安装...${plain}"
            echo -e "-----------------------------------------------------"
            echo ""
            sleep 2
            echo -e "${green}---------------->>>>>>>>>安装进度50%${plain}"
            sleep 3
            echo ""
            echo -e "${green}---------------->>>>>>>>>>>>>>>>>>>>>安装进度100%${plain}"
            echo ""
            sleep 2
            if [[ "${XUI_FROM_SOURCE}" != "1" ]]; then
                wget -N --no-check-certificate -O /usr/local/x-ui-linux-$(arch).tar.gz https://github.com/${XPANEL_REPO}/releases/download/${last_version}/x-ui-linux-$(arch).tar.gz
                if [[ $? -ne 0 ]]; then
                    echo -e "${yellow}Release 下载失败，自动切换为源码构建...${plain}"
                    build_from_source_tarball
                    last_version="源码构建"
                fi
            fi
        else
            last_version=$1
            url="https://github.com/${XPANEL_REPO}/releases/download/${last_version}/x-ui-linux-$(arch).tar.gz"
            echo ""
            echo -e "--------------------------------------------"
            echo -e "${green}---------------->>>>开始安装 X-Panel 完整版$1${plain}"
            echo -e "--------------------------------------------"
            echo ""
            sleep 2
            echo -e "${green}---------------->>>>>>>>>安装进度50%${plain}"
            sleep 3
            echo ""
            echo -e "${green}---------------->>>>>>>>>>>>>>>>>>>>>安装进度100%${plain}"
            echo ""
            sleep 2
            if [[ "${XUI_FROM_SOURCE}" != "1" ]]; then
                wget -N --no-check-certificate -O /usr/local/x-ui-linux-$(arch).tar.gz ${url}
            fi
            if [[ $? -ne 0 ]]; then
                echo -e "${red}下载 X-Panel $1 失败, 请检查此版本是否存在 ${plain}"
                exit 1
            fi
        fi
        wget -O /usr/bin/x-ui-temp https://raw.githubusercontent.com/${XPANEL_REPO}/${XPANEL_BRANCH}/x-ui.sh

        # Stop x-ui service and remove old resources
        if [[ -e /usr/local/x-ui/ ]]; then
            systemctl stop x-ui
            rm /usr/local/x-ui/ -rf
        fi
        
        sleep 3
        echo -e "${green}------->>>>>>>>>>>检查并保存安装目录${plain}"
        echo ""
        tar zxvf x-ui-linux-$(arch).tar.gz
        rm x-ui-linux-$(arch).tar.gz -f
        
        cd x-ui
        chmod +x x-ui
        chmod +x x-ui.sh

        # Check the system's architecture and rename the file accordingly
        if [[ $(arch) == "armv5" || $(arch) == "armv6" || $(arch) == "armv7" ]]; then
            mv bin/xray-linux-$(arch) bin/xray-linux-arm
            chmod +x bin/xray-linux-arm
        fi
        chmod +x x-ui bin/xray-linux-$(arch)

        # Update x-ui cli and se set permission
        mv -f /usr/bin/x-ui-temp /usr/bin/x-ui
        chmod +x /usr/bin/x-ui
        sleep 2
        echo -e "${green}------->>>>>>>>>>>保存成功${plain}"
        sleep 2
        echo ""
        config_after_install

    ssh_forwarding() {
        # 获取 IPv4 和 IPv6 地址
        v4=$(curl -s4m8 http://ip.sb -k)
        v6=$(curl -s6m8 http://ip.sb -k)
        local existing_webBasePath=$(/usr/local/x-ui/x-ui setting -show true | grep -Eo 'webBasePath（访问路径）: .+' | awk '{print $2}') 
        local existing_port=$(/usr/local/x-ui/x-ui setting -show true | grep -Eo 'port（端口号）: .+' | awk '{print $2}') 
        local existing_cert=$(/usr/local/x-ui/x-ui setting -getCert true | grep -Eo 'cert: .+' | awk '{print $2}')
        local existing_key=$(/usr/local/x-ui/x-ui setting -getCert true | grep -Eo 'key: .+' | awk '{print $2}')

        if [[ -n "$existing_cert" && -n "$existing_key" ]]; then
            echo -e "${green}面板已安装证书采用SSL保护${plain}"
            echo ""
            local existing_cert=$(/usr/local/x-ui/x-ui setting -getCert true | grep -Eo 'cert: .+' | awk '{print $2}')
            domain=$(basename "$(dirname "$existing_cert")")
            echo -e "${green}登录访问面板URL: https://${domain}:${existing_port}${green}${existing_webBasePath}${plain}"
        fi
        echo ""
        if [[ -z "$existing_cert" && -z "$existing_key" ]]; then
            echo -e "${red}警告：未找到证书和密钥，面板不安全！${plain}"
            echo ""
            echo -e "${green}------->>>>请按照下述方法设置〔ssh转发〕<<<<-------${plain}"
            echo ""

            # 检查 IP 并输出相应的 SSH 和浏览器访问信息
            if [[ -z $v4 ]]; then
                echo -e "${green}1、本地电脑客户端转发命令：${plain} ${blue}ssh  -L [::]:15208:127.0.0.1:${existing_port}${blue} root@[$v6]${plain}"
                echo ""
                echo -e "${green}2、请通过快捷键【Win + R】调出运行窗口，在里面输入【cmd】打开本地终端服务${plain}"
                echo ""
                echo -e "${green}3、请在终端中成功输入服务器的〔root密码〕，注意区分大小写，用以上命令进行转发${plain}"
                echo ""
                echo -e "${green}4、请在浏览器地址栏复制${plain} ${blue}[::1]:15208${existing_webBasePath}${plain} ${green}进入〔X-Panel面板〕登录界面"
                echo ""
                echo -e "${red}注意：若不使用〔ssh转发〕请为X-Panel面板配置安装证书再行登录管理后台${plain}"
            elif [[ -n $v4 && -n $v6 ]]; then
                echo -e "${green}1、本地电脑客户端转发命令：${plain} ${blue}ssh -L 15208:127.0.0.1:${existing_port}${blue} root@$v4${plain} ${yellow}或者 ${blue}ssh  -L [::]:15208:127.0.0.1:${existing_port}${blue} root@[$v6]${plain}"
                echo ""
                echo -e "${green}2、请通过快捷键【Win + R】调出运行窗口，在里面输入【cmd】打开本地终端服务${plain}"
                echo ""
                echo -e "${green}3、请在终端中成功输入服务器的〔root密码〕，注意区分大小写，用以上命令进行转发${plain}"
                echo ""
                echo -e "${green}4、请在浏览器地址栏复制${plain} ${blue}127.0.0.1:15208${existing_webBasePath}${plain} ${yellow}或者${plain} ${blue}[::1]:15208${existing_webBasePath}${plain} ${green}进入〔X-Panel面板〕登录界面"
                echo ""
                echo -e "${red}注意：若不使用〔ssh转发〕请为X-Panel面板配置安装证书再行登录管理后台${plain}"
            else
                echo -e "${green}1、本地电脑客户端转发命令：${plain} ${blue}ssh -L 15208:127.0.0.1:${existing_port}${blue} root@$v4${plain}"
                echo ""
                echo -e "${green}2、请通过快捷键【Win + R】调出运行窗口，在里面输入【cmd】打开本地终端服务${plain}"
                echo ""
                echo -e "${green}3、请在终端中成功输入服务器的〔root密码〕，注意区分大小写，用以上命令进行转发${plain}"
                echo ""
                echo -e "${green}4、请在浏览器地址栏复制${plain} ${blue}127.0.0.1:15208${existing_webBasePath}${plain} ${green}进入〔X-Panel面板〕登录界面"
                echo ""
                echo -e "${red}注意：若不使用〔ssh转发〕请为X-Panel面板配置安装证书再行登录管理后台${plain}"
                echo ""
            fi
        fi
    }
    # 执行ssh端口转发
    ssh_forwarding

        cp -f x-ui.service /etc/systemd/system/
        systemctl daemon-reload
        systemctl enable x-ui
        systemctl start x-ui
        systemctl stop warp-go >/dev/null 2>&1
        wg-quick down wgcf >/dev/null 2>&1
        ipv4=$(curl -s4m8 ip.p3terx.com -k | sed -n 1p)
        ipv6=$(curl -s6m8 ip.p3terx.com -k | sed -n 1p)
        systemctl start warp-go >/dev/null 2>&1
        wg-quick up wgcf >/dev/null 2A>&1

        echo ""
        echo -e "------->>>>${green}X-Panel 完整版 ${last_version}${plain}<<<<安装成功，正在启动..."
        sleep 1
        echo ""
        echo -e "         ---------------------"
        echo -e "         |${green}X-Panel 控制菜单用法 ${plain}|${plain}"
        echo -e "         |  ${yellow}一个更好的面板   ${plain}|${plain}"   
        echo -e "         | ${yellow}基于Xray Core构建 ${plain}|${plain}"  
        echo -e "--------------------------------------------"
        echo -e "x-ui              - 进入管理脚本"
        echo -e "x-ui start        - 启动 X-Panel 面板"
        echo -e "x-ui stop         - 关闭 X-Panel 面板"
        echo -e "x-ui restart      - 重启 X-Panel 面板"
        echo -e "x-ui status       - 查看 X-Panel 状态"
        echo -e "x-ui settings     - 查看当前设置信息"
        echo -e "x-ui enable       - 启用 X-Panel 开机启动"
        echo -e "x-ui disable      - 禁用 X-Panel 开机启动"
        echo -e "x-ui log          - 查看 X-Panel 运行日志"
        echo -e "x-ui banlog       - 检查 Fail2ban 禁止日志"
        echo -e "x-ui update       - 更新 X-Panel 面板"
        echo -e "x-ui custom       - 自定义 X-Panel 版本"
        echo -e "x-ui install      - 安装 X-Panel 面板"
        echo -e "x-ui uninstall    - 卸载 X-Panel 面板"
        echo -e "--------------------------------------------"
        echo ""
        # if [[ -n $ipv4 ]]; then
        #    echo -e "${yellow}面板 IPv4 访问地址为：${green}http://$ipv4:${config_port}/${config_webBasePath}${plain}"
        # fi
        # if [[ -n $ipv6 ]]; then
        #    echo -e "${yellow}面板 IPv6 访问地址为：${green}http://[$ipv6]:${config_port}/${config_webBasePath}${plain}"
        # fi
        #    echo -e "请自行确保此端口没有被其他程序占用，${yellow}并且确保${red} ${config_port} ${yellow}端口已放行${plain}"
        sleep 3
        echo -e ">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>"
        echo ""
        echo -e "${yellow}----->>>X-Panel面板和Xray启动成功<<<-----${plain}"
    }

    # 设置VPS中的时区/时间为【上海时间】
    sudo timedatectl set-timezone Asia/Shanghai

    install_base
    install_x-ui $1
    echo ""
    echo -e "----------------------------------------------"
    sleep 4
    info=$(/usr/local/x-ui/x-ui setting -show true)
    echo -e "${info}${plain}"
    echo ""
    echo -e "若您忘记了上述面板信息，后期可通过x-ui命令进入脚本${red}输入数字〔10〕选项获取${plain}"
    echo ""
    echo -e "----------------------------------------------"
    echo ""
    sleep 2
    echo -e "${green}安装/更新完成，若在使用过程中有任何问题${plain}"
    echo -e "${yellow}请先描述清楚所遇问题加〔X-Panel面板〕交流群${plain}"
    echo -e "${yellow}在TG群中${red} https://t.me/XUI_CN ${yellow}截图进行反馈${plain}"
    echo ""
    echo -e "----------------------------------------------"
    echo ""
    echo -e "${green}〔X-Panel面板〕项目地址：${yellow}https://github.com/v200dd/X-Panel${plain}" 
    echo ""
    echo -e "${green} 安装说明：${yellow}https://github.com/v200dd/X-Panel/blob/main/安装说明.md${plain}"
    echo ""
    echo -e "----------------------------------------------"
    echo ""
    echo -e "-------------->>>>>>>赞 助 推 广 区<<<<<<<<-------------------"
    echo ""
    echo -e "${green}1、搬瓦工GIA高端线路：${yellow}https://bandwagonhost.com/aff.php?aff=75015${plain}"
    echo ""
    echo -e "${green}2、Dmit高端GIA线路：${yellow}https://www.dmit.io/aff.php?aff=9326${plain}"
    echo ""
    echo -e "${green}3、Gomami亚太顶尖优化线路：${yellow}https://gomami.io/aff.php?aff=174${plain}"
    echo ""
    echo -e "${green}4、ISIF优质亚太优化线路：${yellow}https://cloud.isif.net/login?affiliation_code=333${plain}"
    echo ""
    echo -e "${green}5、ZoroCloud全球优质原生家宽&住宅双lSP，跨境首选：${yellow}https://my.zorocloud.com/aff.php?aff=1072${plain}"
    echo ""
    echo -e "${green}6、三网直连 IEPL / IPLC 直播流量转发：${yellow}https://idc333.top/#register/BCUZXNELNO${plain}"
    echo ""
    echo -e "${green}7、Bagevm优质落地鸡（原生IP全解锁）：${yellow}https://www.bagevm.com/aff.php?aff=754${plain}"
    echo ""
    echo -e "${green}8、白丝云〔4837线路〕实惠量大管饱：${yellow}https://cloudsilk.io/aff.php?aff=706${plain}"
    echo ""
    echo -e "${green}9、RackNerd极致性价比机器：${yellow}https://my.racknerd.com/aff.php?aff=15268&pid=912${plain}"
    echo ""
    echo -e "----------------------------------------------"
    echo ""
}

# 完整版安装逻辑函数 (install_free_version) 结束

# ----------------------------------------------------------
# 脚本主菜单（已移除授权码/激活流程，直接安装完整版）
# ----------------------------------------------------------
main_menu() {
    echo -e "${green}======================================================${plain}"
    echo -e " 欢迎使用 ${yellow}〔X-Panel 面板〕${plain} 一键安装脚本"
    echo -e "${green}======================================================${plain}"
    echo ""
    echo -e "${green}当前仓库：${yellow}${XPANEL_REPO}${plain}（分支：${XPANEL_BRANCH}）"
    echo ""
    install_free_version
}

# ----------------------------------------------------------
# 脚本执行入口
# ----------------------------------------------------------
clear
main_menu
