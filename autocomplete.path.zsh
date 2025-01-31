#!/usr/bin/env zsh

# autocomplete.path.zsh
#
# This script serves two purposes:
# 1. It adds the blockdump binary directory to your PATH
# 2. It sets up ZSH completion for the blockdump command
#
# The script uses ZSH-specific features to:
# - Locate the binary relative to this script's location
# - Set up sophisticated command completion
# - Handle both short (-b) and long (--rpcconnect) options
#
# Usage: Add this line to your .zshrc:
#   source /path/to/autocomplete.path.zsh

# First, we ensure we're running in ZSH. This is important because
# we use ZSH-specific features for completion and path handling.
if [ -n "$BASH_VERSION" ]; then
    echo "Error: This script only works with ZSH" >&2
    return 1
fi

# This function locates the blockdump binary directory by looking
# relative to this script's location. It uses ${(%):-%x} which is
# a ZSH-specific way to get the current script's path.
function _find__blockdump_dir() {
    local script_dir="$(cd "$(dirname "${(%):-%x}")" && pwd)"
    
    # Try to find the binary in ../bin relative to this script
    if [[ -f "${script_dir}/../bin/blockdump" ]]; then
        echo "${script_dir}/../bin"
        return 0
    fi
    
    # If not found above, check ./bin relative to this script
    if [[ -f "${script_dir}/bin/blockdump" ]]; then
        echo "${script_dir}/bin"
        return 0
    fi
    
    return 1
}

# Set up the PATH to include the blockdump binary directory.
# We only add it if it's not already there to avoid duplicates.
_blockdump_dir="$(_find__blockdump_dir)"
if [[ $? -eq 0 ]]; then
    # Convert to absolute path to avoid any relative path issues
    _blockdump_dir="$(cd "$_blockdump_dir" && pwd)"
    
    if [[ ":$PATH:" != *":$_blockdump_dir:"* ]]; then
        export PATH="$_blockdump_dir:$PATH"
        echo "Added blockdump directory to PATH: $_blockdump_dir" >&2
    fi
else
    echo "Warning: Could not locate blockdump binary directory" >&2
    return 1
fi

# Load the completion system if it's not already loaded.
# This is required for the compdef command to work.
if ! whence -w compdef >/dev/null; then
    autoload -Uz compinit
    compinit
fi

# This function handles command completion for blockdump.
# It provides completion for:
# - Command-line options (both short and long form)
# - Subcommands (like genesisblock, allblocks)
# - Arguments for specific subcommands
function _blockdump() {
    local curcontext="$curcontext" state line ret=1
    typeset -A opt_args

    # Define all the command-line options.
    # Each option is defined with its help text and completion type.
    local -a args
    args=(
        # Help option - should always be available
        '(- : *)--help[Show help information]'
        # Connection options with their default values
        '--rpcconnect=[Send commands to node running on IP]:host:_hosts'
        '--rpcport=[Connect to JSON-RPC on port (default: 30174)]:port'
        '--rpcuser=[Username for JSON-RPC connections]:username'
        '--rpcpassword=[Password for JSON-RPC connections]:password'
        '--rpcclienttimeout=[Timeout during HTTP requests (default: 900)]:timeout'
        '--rpcretrylimit=[Retry limit for JSON-RPC requests (default: 3)]:limit'
        '--rpcretrybackoffmax=[Maximum backoff time (default: 300)]:seconds'
        '--rpcretrybackoffmin=[Minimum backoff time (default: 1)]:seconds'
        # Basic options
        '-b[Binary output]'
        '-f[Output to file]:file:_files'
        # Command and argument handling
        '1: :->cmds'
        '*:: :->args'
    )

    # Process the arguments
    _arguments -s -S : $args && ret=0

    # Handle different completion contexts based on where the cursor is
    case $state in
        cmds)
            local -a commands
            commands=(
                'genesisblock:Retrieve the first block in the blockchain'
                'allblocks:Retrieve all blocks in the blockchain'
                'specificblock:Usage: specificblock <block_id>'
                'blockrange:Usage: blockrange <start_id> <end_id>'
                'randomblock:Retrieve a random block from the blockchain'
                'randomblocksample:Usage: randomblocksample <count>'
            )
            _describe -t commands 'blockdump commands' commands && ret=0
            ;;
        args)
            case $line[1] in
                specificblock)
                    if (( CURRENT == 2 )); then
                        _message -r 'blockdump specificblock <block_id> - Enter block ID number' && ret=0
                    fi
                    ;;
                blockrange)
                    case $CURRENT in
                        2)
                            _message -r 'blockdump blockrange <start_id> <end_id> - Enter starting block ID' && ret=0
                            ;;
                        3)
                            _message -r 'blockdump blockrange '$line[2]' <end_id> - Enter ending block ID' && ret=0
                            ;;
                    esac
                    ;;
                randomblocksample)
                    if (( CURRENT == 2 )); then
                        _message -r 'blockdump randomblocksample <count> - Enter number of blocks to sample' && ret=0
                    fi
                    ;;
                genesisblock|allblocks|randomblock)
                    _message 'This command takes no additional arguments' && ret=1
                    ;;
            esac
            ;;
    esac

    return ret
}

# Register our completion function for the blockdump command
compdef _blockdump blockdump

# Verify that everything is set up correctly
if command -v blockdump >/dev/null 2>&1; then
    echo "blockdump is now available in your PATH and completions are configured" >&2
else
    echo "Warning: blockdump command not found in PATH despite setup attempts" >&2
    return 1
fi
