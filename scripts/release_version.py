"""One Android upgrade sequence for every Unified release workflow channel."""
import os


def android_version_code(run_number):
    run = int(run_number)
    # Above all MattRip version codes published before this migration. Both
    # numbered and main builds MUST use this same workflow counter thereafter.
    code = 1_000_000_000 + run
    if run < 1 or code > 2_100_000_000:
        raise ValueError('Unified release run number is outside Android versionCode range')
    return code


if __name__ == '__main__':
    print(android_version_code(os.environ['GITHUB_RUN_NUMBER']))
