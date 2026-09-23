#!/usr/bin/env bash
# Garde-fou PreToolUse : refuse toute suppression hors du projet et du
# dossier temporaire.
#
# Reçoit sur stdin le JSON de l'appel d'outil (Bash ou PowerShell), repère les
# commandes qui suppriment (rm, rmdir, unlink, shred, find -delete,
# find -exec rm, rsync --delete, Remove-Item et ses alias), résout chaque cible
# et :
#   - refuse (deny) si une cible sort du projet ou du dossier temporaire, ou
#     désigne l'une de ces racines elle-même ;
#   - demande confirmation (ask) si une cible ne peut pas être résolue
#     statiquement : variable, substitution de commande, xargs ;
#   - se tait sinon, laissant les permissions ordinaires décider.
#
# Ne dépend que de bash et de coreutils : jq n'est pas garanti sous Git Bash.
# L'analyse est volontairement prudente : un faux positif coûte une
# confirmation, un faux négatif peut coûter un disque.

set -u

input=$(cat)

# Filtre rapide : la plupart des commandes ne suppriment rien.
if ! grep -Eqi '(^|[^[:alnum:]_.-])(rm|rmdir|unlink|shred|del|erase|rd|ri|remove-item|trash|trash-put)([^[:alnum:]_.-]|$)|-delete' <<<"$input"; then
    exit 0
fi

# json_string KEY : décode la première valeur chaîne associée à KEY.
json_string() {
    local key=$1 rest out="" c i n
    rest=${input#*\""$key"\"}
    [[ $rest == "$input" ]] && return 1
    rest=${rest#*:}
    rest=${rest#"${rest%%[![:space:]]*}"}
    [[ ${rest:0:1} == '"' ]] || return 1
    n=${#rest}
    for ((i = 1; i < n; i++)); do
        c=${rest:i:1}
        if [[ $c == '\' ]]; then
            ((i++))
            c=${rest:i:1}
            case $c in
                n) out+=$'\n' ;;
                t) out+=$'\t' ;;
                r) ;;
                b | f) ;;
                u) out+="?"; ((i += 4)) ;;
                *) out+=$c ;;
            esac
        elif [[ $c == '"' ]]; then
            printf '%s' "$out"
            return 0
        else
            out+=$c
        fi
    done
    return 1
}

command_text=$(json_string command) || exit 0
cwd=$(json_string cwd) || cwd=$PWD

# normalize PATH : ramène les chemins Windows à la forme de Git Bash
# (C:\x → /c/x) pour que les comparaisons se fassent dans un seul espace.
normalize() {
    local p=${1//\\//}
    if [[ $p =~ ^([A-Za-z]):(/.*)?$ ]]; then
        local drive=${BASH_REMATCH[1],,}
        p="/$drive${BASH_REMATCH[2]}"
    fi
    printf '%s' "$p"
}

resolve() { realpath -m -- "$1" 2>/dev/null || printf '%s' "$1"; }

roots=()
add_root() {
    [[ -n ${1:-} ]] || return
    local r
    r=$(resolve "$(normalize "$1")")
    [[ $r == / || -z $r ]] && return
    roots+=("$r")
}
add_root "${CLAUDE_PROJECT_DIR:-}"
add_root /tmp
add_root "${TMPDIR:-}"
add_root "${TEMP:-}"
add_root "${TMP:-}"
if [[ ${#roots[@]} -eq 0 || -z ${CLAUDE_PROJECT_DIR:-} ]]; then
    # Sans racine de projet, rien ne peut être vérifié : prudence.
    add_root "$cwd"
fi

denied=()
unsure=()

DYN=$'\x01'

# check_target WORD CWD [CONTENTS] : classe une cible de suppression.
#
# CONTENTS vaut 1 quand la commande n'atteint que le contenu de la cible
# (motif, point de départ de find) : la racine elle-même est alors admise.
check_target() {
    local word=$1 dir=$2 contents=${3:-0} t prefix abs root
    if [[ $word == *"$DYN"* ]]; then
        unsure+=("${word//$DYN/} (valeur calculée à l'exécution)")
        return
    fi
    if [[ $dir == "$DYN" ]]; then
        unsure+=("$word (répertoire courant inconnu)")
        return
    fi
    t=$(normalize "$word")
    [[ $t == "~" || $t == "~/"* ]] && t="$HOME${t:1}"
    # Un motif désigne au plus le dossier qui précède son premier joker.
    if [[ $t == *[*?[{]* ]]; then
        contents=1
        prefix=${t%%[*?[{]*}
        if [[ $prefix == */ ]]; then
            t=${prefix%/}
            [[ -z $t ]] && t=/
        elif [[ $prefix == */* ]]; then
            t=${prefix%/*}
            [[ -z $t ]] && t=/
        else
            t=.
        fi
    fi
    [[ $t != /* ]] && t="$dir/$t"
    abs=$(resolve "$t")
    for root in "${roots[@]}"; do
        if [[ $abs == "$root"/* ]] || { ((contents)) && [[ $abs == "$root" ]]; }; then
            return
        fi
    done
    denied+=("$word → $abs")
}

is_delete_command() {
    case ${1,,} in
        rm | rmdir | unlink | shred | del | erase | rd | ri | remove-item | trash | trash-put) return 0 ;;
    esac
    return 1
}

# check_segment CWD WORD... : analyse une commande simple. Met à jour la
# variable seg_cwd si la commande change de répertoire.
check_segment() {
    local dir=$1
    shift
    local words=("$@") name i w
    # Préfixes transparents : affectations, sudo, env, command…
    while ((${#words[@]} > 0)); do
        w=${words[0]}
        if [[ $w =~ ^[A-Za-z_][A-Za-z0-9_]*= ]]; then
            words=("${words[@]:1}")
            continue
        fi
        case ${w##*/} in
            sudo | doas | command | builtin | exec | nice | nohup | time | env | stdbuf | timeout)
                words=("${words[@]:1}")
                # Options du préfixe et, pour timeout, sa durée.
                while ((${#words[@]} > 0)) && [[ ${words[0]} == -* || ${words[0]} =~ ^[0-9.]+[smhd]?$ ]]; do
                    words=("${words[@]:1}")
                done
                continue
                ;;
        esac
        break
    done
    ((${#words[@]} == 0)) && return

    name=${words[0]//$DYN/}
    name=${name#\\}
    name=${name##*/}
    name=${name%.exe}

    case ${name,,} in
        cd | pushd | set-location | sl | chdir)
            if ((${#words[@]} < 2)); then
                seg_cwd=$HOME
            elif [[ ${words[1]} == *"$DYN"* || $dir == "$DYN" ]]; then
                seg_cwd=$DYN
            else
                local target
                target=$(normalize "${words[1]}")
                [[ $target == "~" || $target == "~/"* ]] && target="$HOME${target:1}"
                [[ $target != /* ]] && target="$dir/$target"
                seg_cwd=$(resolve "$target")
            fi
            return
            ;;
        bash | sh | zsh | dash | ksh)
            for ((i = 1; i < ${#words[@]}; i++)); do
                if [[ ${words[i]} == -*c* && ${words[i]} != --* ]]; then
                    ((i + 1 < ${#words[@]})) && analyse "${words[i + 1]//$DYN/}" "$dir"
                    return
                fi
            done
            return
            ;;
        eval)
            analyse "${words[*]:1}" "$dir"
            return
            ;;
        xargs | parallel)
            for ((i = 1; i < ${#words[@]}; i++)); do
                if is_delete_command "${words[i]##*/}"; then
                    unsure+=("${words[*]} (cibles lues sur l'entrée standard)")
                    return
                fi
            done
            return
            ;;
        find)
            local starts=() deleting=0 in_expr=0
            for ((i = 1; i < ${#words[@]}; i++)); do
                w=${words[i]}
                if ((in_expr == 0)) && [[ $w != -* && $w != '(' && $w != '!' ]]; then
                    starts+=("$w")
                    continue
                fi
                in_expr=1
                case $w in
                    -delete) deleting=1 ;;
                    -exec | -execdir | -ok | -okdir)
                        is_delete_command "${words[i + 1]##*/}" && deleting=1
                        ;;
                esac
            done
            if ((deleting)); then
                ((${#starts[@]} == 0)) && starts=(.)
                for w in "${starts[@]}"; do check_target "$w" "$dir" 1; done
            fi
            return
            ;;
        rsync)
            local deleting=0 last=""
            for ((i = 1; i < ${#words[@]}; i++)); do
                [[ ${words[i]} == --delete* || ${words[i]} == --remove-source-files ]] && deleting=1
                [[ ${words[i]} != -* ]] && last=${words[i]}
            done
            ((deleting)) && [[ -n $last && $last != *:* ]] && check_target "$last" "$dir"
            return
            ;;
    esac

    is_delete_command "$name" || return

    local options_done=0
    for ((i = 1; i < ${#words[@]}; i++)); do
        w=${words[i]}
        if ((options_done == 0)); then
            [[ $w == -- ]] && { options_done=1; continue; }
            [[ $w == -* ]] && continue
        fi
        check_target "$w" "$dir"
    done
}

# analyse TEXT CWD : découpe un texte de shell en commandes simples.
#
# Le découpage suit les guillemets, les échappements et les opérateurs ; il
# marque d'un octet DYN les mots dont la valeur dépend de l'exécution ($, `).
# Les cibles de redirection sont écartées : « 2>/dev/null » ne supprime rien.
analyse() {
    local text=$1
    local seg_cwd=$2
    local n=${#text} i c word="" has_word=0 quote="" skip_next=0
    local words=()

    end_word() {
        if ((has_word)); then
            if ((skip_next)); then
                skip_next=0
            else
                words+=("$word")
            fi
        fi
        word=""
        has_word=0
    }
    end_segment() {
        end_word
        if ((${#words[@]} > 0)); then
            check_segment "$seg_cwd" "${words[@]}"
        fi
        words=()
        skip_next=0
    }

    for ((i = 0; i < n; i++)); do
        c=${text:i:1}
        if [[ $quote == "'" ]]; then
            if [[ $c == "'" ]]; then quote=""; else word+=$c; fi
            continue
        fi
        if [[ $quote == '"' ]]; then
            case $c in
                '"') quote="" ;;
                '\')
                    ((i++))
                    word+=${text:i:1}
                    ;;
                '$' | '`') word+="$DYN$c" ;;
                *) word+=$c ;;
            esac
            continue
        fi
        case $c in
            "'" | '"') quote=$c; has_word=1 ;;
            '\')
                ((i++))
                if [[ ${text:i:1} != $'\n' ]]; then
                    word+=${text:i:1}
                    has_word=1
                fi
                ;;
            ' ' | $'\t') end_word ;;
            $'\n' | ';' | '&' | '|' | '(' | ')') end_segment ;;
            '>' | '<')
                # « 2>… » : le numéro de descripteur n'est pas un argument.
                if ((has_word)) && [[ $word =~ ^[0-9]+$ ]]; then
                    word=""
                    has_word=0
                fi
                end_word
                [[ ${text:i+1:1} == '>' || ${text:i+1:1} == '&' ]] && ((i++))
                skip_next=1
                ;;
            '#')
                if ((has_word)); then
                    word+=$c
                else
                    while ((i + 1 < n)) && [[ ${text:i+1:1} != $'\n' ]]; do ((i++)); done
                fi
                ;;
            '$' | '`') word+="$DYN$c"; has_word=1 ;;
            *) word+=$c; has_word=1 ;;
        esac
    done
    end_segment
}

analyse "$command_text" "$(resolve "$(normalize "$cwd")")"

emit() {
    local decision=$1 reason=$2
    reason=${reason//\\/\\\\}
    reason=${reason//\"/\\\"}
    reason=${reason//$'\n'/\\n}
    reason=${reason//$'\t'/ }
    printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"%s","permissionDecisionReason":"%s"}}\n' \
        "$decision" "$reason"
}

allowed="Zones autorisées : ${roots[*]}"
if ((${#denied[@]} > 0)); then
    emit deny "Suppression refusée hors du projet et du dossier temporaire : $(printf '%s ; ' "${denied[@]}")$allowed. Restreindre la cible au projet ou à /tmp."
elif ((${#unsure[@]} > 0)); then
    emit ask "Cible de suppression non vérifiable statiquement : $(printf '%s ; ' "${unsure[@]}")$allowed."
fi
exit 0
