cd /path/to/project/builtin/gen
```
rm -rf ./compiled/
docker run --rm -w /source -v $PWD:/source -v $PWD/compiled:/source/compiled -t ethereum/solc:0.4.24 --optimize-runs 200 --overwrite --bin-runtime --bin --abi -o ./compiled meter.sol executor.sol extension.sol measure.sol params.sol prototype.sol meternative.sol meter-erc20.sol
go-bindata -nometadata -ignore=_ -pkg gen -o bindata.go compiled/
```
cd -