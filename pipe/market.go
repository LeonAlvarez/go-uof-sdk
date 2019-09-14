package pipe

import (
	"sync"
	"time"

	"github.com/minus5/uof"
)

type marketsApi interface {
	Markets(lang uof.Lang) (uof.MarketDescriptions, error)
	MarketVariant(lang uof.Lang, marketID int, variant string) (uof.MarketDescriptions, error)
}

type markets struct {
	api       marketsApi
	languages []uof.Lang
	em        *expireMap
	errc      chan<- error
	out       chan<- *uof.Message
	subProcs  *sync.WaitGroup
}

// getting all markets on the start
func Markets(api marketsApi, languages []uof.Lang) stage {
	var wg sync.WaitGroup
	m := &markets{
		api:       api,
		languages: languages,
		em:        newExpireMap(24 * time.Hour),
		subProcs:  &wg,
	}
	return StageWithSubProcesses(m.loop)
}

func (s *markets) loop(in <-chan *uof.Message, out chan<- *uof.Message, errc chan<- error) *sync.WaitGroup {
	s.out, s.errc = out, errc

	s.getAll()
	for m := range in {
		out <- m
		if m.Is(uof.MessageTypeOddsChange) {
			m.OddsChange.EachVariantMarket(s.variantMarket)
		}
	}
	return s.subProcs
}

func (s *markets) getAll() {
	s.subProcs.Add(len(s.languages))

	for _, lang := range s.languages {
		go func(lang uof.Lang) {
			defer s.subProcs.Done()

			ms, err := s.api.Markets(lang)
			if err != nil {
				s.errc <- err
				return
			}
			s.out <- uof.NewMarketsMessage(lang, ms)
		}(lang)
	}
}

func (s *markets) variantMarket(marketID int, variant string) {
	s.subProcs.Add(len(s.languages))

	for _, lang := range s.languages {
		go func(lang uof.Lang) {
			defer s.subProcs.Done()

			key := uof.UIDWithLang(uof.Hash(variant)<<32|marketID, lang)
			if s.em.fresh(key) {
				return
			}
			ms, err := s.api.MarketVariant(lang, marketID, variant)
			if err != nil {
				s.errc <- err
				return
			}
			s.out <- uof.NewMarketsMessage(lang, ms)
			s.em.insert(key)
		}(lang)
	}
}
