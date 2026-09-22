MANIFEST = {
    "title": "Комиссия минус VALUE пунктов",
    "description": "Из ставки по уровню вычитается VALUE процентных пунктов, " +
                   "но не ниже нуля: 7 % минус 5 пунктов — 2 %.",
    "defaults": {"VALUE": 5},
}

def rate(f):
    return max(0, f.level_percent - f.config["VALUE"])
