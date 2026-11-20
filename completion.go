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
            COMPREPLY=($(compgen -W "next prev pin unpin save info catalog apply set reapply daemon version completion -config" -- "$cur")) ;;
        next)
            COMPREPLY=($(compgen -W "--force spotlight bing wallhaven local" -- "$cur")) ;;
        save|pin|unpin)
            COMPREPLY=($(compgen -W "save pin unpin" -- "$cur")) ;;
        prev|apply)
            COMPREPLY=($(compgen -W "--force" -- "$cur")) ;;
        set)
            if [[ $cur == -* ]]; then
                COMPREPLY=($(compgen -W "--force --no-pin" -- "$cur"))
            else
                compopt -o filenames 2>/dev/null
                COMPREPLY=($(compgen -f -- "$cur"))
            fi ;;
        info)
            COMPREPLY=($(compgen -W "-json" -- "$cur")) ;;
        catalog)
            COMPREPLY=($(compgen -W "-json -saved -n" -- "$cur")) ;;
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
        'catalog:every wallpaper shown so far, longest on screen first'
        'apply:show a wallpaper from the catalog again'
        'set:show an image file of your own and pin it'
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
                        '1::source:(spotlight bing wallhaven local)' ;;
                prev)
                    _arguments '(-f --force)'{-f,--force}'[change even if pinned]' ;;
                apply)
                    _arguments '(-f --force)'{-f,--force}'[change even if pinned]' \
                        '1:number in the catalog or image ID:' ;;
                set)
                    _arguments '(-f --force)'{-f,--force}'[change even if pinned]' \
                        '--no-pin[let the daemon change it]' \
                        '1:image file:_files' ;;
                save|pin|unpin)
                    _values 'also' save pin unpin ;;
                info)
                    _arguments '-json[print JSON]' ;;
                catalog)
                    _arguments '-json[print JSON]' '-saved[only saved wallpapers]' \
                        '-n[how many to list, 0 for all (default 10)]:count:' ;;
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
set -l commands next prev pin unpin save info catalog apply set reapply daemon version completion

complete -c hyprautopaper -f
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -o config -r -F -d 'Config file'

complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a next -d 'Next wallpaper, or a new one from SOURCE'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a prev -d 'Previous wallpaper from the history'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a pin -d 'Keep the current wallpaper'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a unpin -d 'Let the wallpaper change again'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a save -d 'Copy the current wallpaper to save_dir'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a info -d 'Show the current wallpaper'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a catalog -d 'Every wallpaper shown so far'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a apply -d 'Show a wallpaper from the catalog again'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a set -d 'Show an image file of your own and pin it'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a reapply -d 'Show the current wallpaper again'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a daemon -d 'Change the wallpaper every interval'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a version -d 'Print the version'
complete -c hyprautopaper -n "not __fish_seen_subcommand_from $commands" -a completion -d 'Print a shell completion script'

complete -c hyprautopaper -n "__fish_seen_subcommand_from save pin unpin" -a 'save pin unpin' -d 'Also do this'
complete -c hyprautopaper -n "__fish_seen_subcommand_from next prev apply set" -s f -l force -d 'Change even if pinned'
complete -c hyprautopaper -n "__fish_seen_subcommand_from next; and not __fish_seen_subcommand_from spotlight bing wallhaven local" -a 'spotlight bing wallhaven local' -d 'Source'
complete -c hyprautopaper -n "__fish_seen_subcommand_from set" -F -d 'Image file'
complete -c hyprautopaper -n "__fish_seen_subcommand_from set" -l no-pin -d 'Let the daemon change it'
complete -c hyprautopaper -n "__fish_seen_subcommand_from info catalog" -o json -d 'Print JSON'
complete -c hyprautopaper -n "__fish_seen_subcommand_from catalog" -o saved -d 'Only saved wallpapers'
complete -c hyprautopaper -n "__fish_seen_subcommand_from catalog" -o n -x -d 'How many to list, 0 for all (default 10)'
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
