package main

import "fmt"

// Shell completion scripts, printed by "hyprautopaper completion SHELL".
// Keep the command and source lists in sync with usage and validSources.

const bashCompletion = `# bash completion for hyprautopaper
_hyprautopaper() {
    local cur prev cmd i
    cur=${COMP_WORDS[COMP_CWORD]}
    prev=${COMP_WORDS[COMP_CWORD-1]}

    if [[ $prev == -config || $prev == --config ]]; then
        compopt -o filenames 2>/dev/null
        COMPREPLY=($(compgen -f -- "$cur"))
        return
    fi

    # Find the command, skipping "-config FILE".
    for ((i = 1; i < COMP_CWORD; i++)); do
        case ${COMP_WORDS[i]} in
            -config|--config) ((i++)) ;;
            -*) ;;
            *) cmd=${COMP_WORDS[i]}; break ;;
        esac
    done

    case $cmd in
        "")
            COMPREPLY=($(compgen -W "next prev pin unpin save info reapply daemon version completion -config" -- "$cur")) ;;
        next)
            COMPREPLY=($(compgen -W "--force spotlight bing wallhaven" -- "$cur")) ;;
        prev)
            COMPREPLY=($(compgen -W "--force" -- "$cur")) ;;
        info)
            COMPREPLY=($(compgen -W "-json" -- "$cur")) ;;
        completion)
            COMPREPLY=($(compgen -W "bash zsh fish" -- "$cur")) ;;
    esac
}
complete -F _hyprautopaper hyprautopaper
`

const zshCompletion = `#compdef hyprautopaper
# zsh completion for hyprautopaper

_hyprautopaper() {
    local -a commands
    commands=(
        'next:next wallpaper, or a new one from SOURCE'
        'prev:previous wallpaper from the history'
        'pin:keep the current wallpaper'
        'unpin:let the wallpaper change again'
        'save:copy the current wallpaper to save_dir'
        'info:show the current wallpaper'
        'reapply:show the current wallpaper again'
        'daemon:change the wallpaper every interval'
        'version:print the version'
        'completion:print a shell completion script'
    )

    _arguments -C \
        '-config[config file]:config file:_files' \
        '1: :->command' \
        '*:: :->args'

    case $state in
        command)
            _describe -t commands 'hyprautopaper command' commands ;;
        args)
            case $words[1] in
                next)
                    _arguments '(-f --force)'{-f,--force}'[change even if pinned]' \
                        '1::source:(spotlight bing wallhaven)' ;;
                prev)
                    _arguments '(-f --force)'{-f,--force}'[change even if pinned]' ;;
                info)
                    _arguments '-json[print JSON]' ;;
                completion)
                    _arguments '1:shell:(bash zsh fish)' ;;
            esac ;;
    esac
}

if [[ $zsh_eval_context[-1] == loadautofunc ]]; then
    _hyprautopaper "$@"
else
    compdef _hyprautopaper hyprautopaper
fi
`

const fishCompletion = `# fish completion for hyprautopaper
set -l commands next prev pin unpin save info reapply daemon version completion

complete -c hyprautopaper -f
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -o config -r -F -d 'Config file'

complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a next -d 'Next wallpaper, or a new one from SOURCE'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a prev -d 'Previous wallpaper from the history'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a pin -d 'Keep the current wallpaper'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a unpin -d 'Let the wallpaper change again'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a save -d 'Copy the current wallpaper to save_dir'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a info -d 'Show the current wallpaper'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a reapply -d 'Show the current wallpaper again'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a daemon -d 'Change the wallpaper every interval'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a version -d 'Print the version'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a completion -d 'Print a shell completion script'

complete -c hyprautopaper -n "__fish_seen_subcommand_from next prev" -s f -l force -d 'Change even if pinned'
complete -c hyprautopaper -n "__fish_seen_subcommand_from next; and not __fish_seen_subcommand_from spotlight bing wallhaven" -a 'spotlight bing wallhaven' -d 'Source'
complete -c hyprautopaper -n "__fish_seen_subcommand_from info" -o json -d 'Print JSON'
complete -c hyprautopaper -n "__fish_seen_subcommand_from completion" -a 'bash zsh fish' -d 'Shell'
`

func printCompletion(shell string) error {
	scripts := map[string]string{"bash": bashCompletion, "zsh": zshCompletion, "fish": fishCompletion}
	s, ok := scripts[shell]
	if !ok {
		return usageError{fmt.Errorf("unknown shell %q (valid: bash, zsh, fish)", shell)}
	}
	fmt.Print(s)
	return nil
}
